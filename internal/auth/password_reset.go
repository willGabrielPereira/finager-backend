package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/mailer"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
	"github.com/willGabrielPereira/finager-backend/pkg/validator"
)

// passwordResetTTL é a validade do link de redefinição (o template diz "30 minutos").
const passwordResetTTL = 30 * time.Minute

type forgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password" validate:"required,min=8,max=72"`
}

// forgotPasswordResponse é o corpo FIXO do forgot — idêntico exista ou não o e-mail.
var forgotPasswordResponse = map[string]string{
	"message": "Se o e-mail estiver cadastrado, você receberá um link para redefinir a senha.",
}

// PasswordResetHandler atende a recuperação de senha (forgot/reset), sem autenticação.
type PasswordResetHandler struct {
	service      *Service
	userRepo     *repository.UserRepository
	resetRepo    *repository.PasswordResetRepository
	refreshRepo  *repository.RefreshTokenRepository
	sender       mailer.Sender // nil = e-mail desligado
	emailLimiter rateLimiter   // limite por e-mail, aplicado dentro da goroutine
	appBaseURL   string
}

func NewPasswordResetHandler(
	svc *Service,
	userRepo *repository.UserRepository,
	resetRepo *repository.PasswordResetRepository,
	refreshRepo *repository.RefreshTokenRepository,
	sender mailer.Sender,
	emailLimiter rateLimiter,
	appBaseURL string,
) *PasswordResetHandler {
	return &PasswordResetHandler{
		service:      svc,
		userRepo:     userRepo,
		resetRepo:    resetRepo,
		refreshRepo:  refreshRepo,
		sender:       sender,
		emailLimiter: emailLimiter,
		appBaseURL:   strings.TrimRight(appBaseURL, "/"),
	}
}

// Forgot handles POST /auth/password/forgot.
//
// @Summary      Esqueci minha senha
// @Description  Envia (em segundo plano) um link de redefinição de senha. A resposta é sempre 202 com o mesmo corpo, exista ou não o e-mail.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      forgotPasswordRequest  true  "E-mail da conta"
// @Success      202   {object}  map[string]string
// @Failure      422   {object}  response.ValidationResponse
// @Failure      429   {object}  map[string]string
// @Router       /auth/password/forgot [post]
func (h *PasswordResetHandler) Forgot(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload de requisição malformado"})
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	// Responde ANTES de qualquer consulta: tempo e corpo não revelam se o e-mail existe.
	response.JSON(w, http.StatusAccepted, forgotPasswordResponse)

	if h.sender == nil {
		return
	}
	email := req.Email
	// mailer.Go usa context.Background() + timeout: r.Context() é cancelado ao fim da resposta.
	mailer.Go("password_reset", 15*time.Second, func(ctx context.Context) {
		h.sendResetEmail(ctx, email)
	})
}

func (h *PasswordResetHandler) sendResetEmail(ctx context.Context, email string) {
	if !h.emailLimiter.Allow(email) {
		slog.Warn("email.rate_limited", "event", "password_reset", "to", mailer.Mask(email))
		return
	}
	user, err := h.userRepo.FindByEmail(ctx, email)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("email.failed", "event", "password_reset", "to", mailer.Mask(email), "err", err)
		}
		return
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		slog.Error("email.failed", "event", "password_reset", "to", mailer.Mask(email), "err", err)
		return
	}
	raw := base64.RawURLEncoding.EncodeToString(b) // sem '=': seguro em fragmento/querystring

	if err := h.resetRepo.Create(ctx, user.ID, h.service.HashToken(raw), time.Now().Add(passwordResetTTL)); err != nil {
		slog.Error("email.failed", "event", "password_reset", "to", mailer.Mask(email), "err", err)
		return
	}

	// Fragmento (#), não query: o token não vaza via Referer nem logs de servidor.
	subject, body, err := mailer.Render("password_reset", map[string]string{
		"Login": user.Login,
		"Link":  h.appBaseURL + "/redefinir-senha#token=" + raw,
	})
	if err != nil {
		slog.Error("email.failed", "event", "password_reset", "to", mailer.Mask(email), "err", err)
		sentry.CaptureException(err)
		return
	}
	if err := h.sender.Send(ctx, mailer.Message{To: []string{user.Email}, Subject: subject, HTML: body}); err != nil {
		slog.Error("email.failed", "event", "password_reset", "to", mailer.Mask(email), "err", err)
		sentry.CaptureException(fmt.Errorf("email failed (password_reset): %w", err))
		return
	}
	slog.Info("email.sent", "event", "password_reset", "to", mailer.Mask(email))
}

// Reset handles POST /auth/password/reset.
//
// @Summary      Redefinir senha
// @Description  Troca a senha usando o token recebido por e-mail (uso único). Revoga todas as sessões (refresh tokens) do usuário.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body  resetPasswordRequest  true  "Token e nova senha"
// @Success      204
// @Failure      400  {object}  map[string]interface{}
// @Failure      422  {object}  response.ValidationResponse
// @Failure      429  {object}  map[string]string
// @Failure      500  {object}  map[string]interface{}
// @Router       /auth/password/reset [post]
func (h *PasswordResetHandler) Reset(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload de requisição malformado"})
		return
	}
	// Valida a senha ANTES de consumir: senha fraca não queima o token.
	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}
	if req.Token == "" {
		response.Error(w, http.StatusBadRequest, "E_INVALID_TOKEN", "Link inválido ou expirado")
		return
	}

	userID, err := h.resetRepo.Consume(r.Context(), h.service.HashToken(req.Token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusBadRequest, "E_INVALID_TOKEN", "Link inválido ou expirado")
			return
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "erro interno")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "erro interno")
		return
	}
	if err := h.userRepo.UpdatePassword(r.Context(), userID, string(hash)); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "erro interno")
		return
	}
	// Derruba todas as sessões: quem roubou a senha antiga perde o acesso.
	if err := h.refreshRepo.RevokeAllByUser(r.Context(), userID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "erro interno")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
