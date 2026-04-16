package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
	"github.com/willGabrielPereira/finager-backend/pkg/validator"
)

// — Request / response types ───────────────────────────────────────────────────

type loginRequest struct {
	Login    string `json:"login"    validate:"required"`
	Password string `json:"password" validate:"required"`
}

type registerRequest struct {
	Login      string `json:"login"       validate:"required,min=4"`
	Password   string `json:"password"    validate:"required,min=8"`
	FamilyName string `json:"family_name" validate:"omitempty,min=2"`
}

// loginResponse contains both tokens returned on successful authentication.
type loginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"` // optional — revokes a specific refresh token
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// errorResponse is returned on any failure.
type errorResponse struct {
	Error string `json:"error"`
}

// — Handler ────────────────────────────────────────────────────────────────────

// Handler holds dependencies needed by auth HTTP handlers.
type Handler struct {
	service          *Service
	userRepo         *repository.UserRepository
	familyRepo       *repository.FamilyRepository
	refreshRepo      *repository.RefreshTokenRepository
	blocklistRepo    *repository.BlocklistRepository
	refreshExpiresIn time.Duration
}

// NewHandler creates an auth Handler with all required dependencies.
func NewHandler(
	svc *Service,
	userRepo *repository.UserRepository,
	familyRepo *repository.FamilyRepository,
	refreshRepo *repository.RefreshTokenRepository,
	blocklistRepo *repository.BlocklistRepository,
	refreshExpiresHours int,
) *Handler {
	return &Handler{
		service:          svc,
		userRepo:         userRepo,
		familyRepo:       familyRepo,
		refreshRepo:      refreshRepo,
		blocklistRepo:    blocklistRepo,
		refreshExpiresIn: time.Duration(refreshExpiresHours) * time.Hour,
	}
}

// — Endpoints ──────────────────────────────────────────────────────────────────

// Register handles POST /auth/register
// @Summary      Cadastro de Usuário
// @Description  Cadastra um novo usuário e automaticamente instancializa uma Familia root vinculada a ele.
// @Tags         auth
// @Produce      json
// @Param        body body registerRequest true "Dados de cadastro"
// @Success      201  {object} loginResponse
// @Failure      422  {object} response.ValidationResponse
// @Failure      500  {object} map[string]interface{}
// @Router       /auth/register [post]
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload de requisição malformado"})
		return
	}

	// Delega pra Engine Global extrair todas as falhas num Array VineJS
	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	// 1. Checa unicidade
	exists, _ := h.userRepo.FindByLogin(r.Context(), req.Login)
	if exists != nil {
		response.Validations(w, response.ValidationError{Field: "login", Rule: "unique", Message: "Este login já está sendo utilizado"})
		return
	}

	// 2. Hash Password
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha interna ao gerar hash")
		return
	}

	// 3. Cria a Familia (Injeção dinamica se nulo)
	fname := req.FamilyName
	if fname == "" {
		fname = "Família de " + req.Login
	}

	family := &models.Family{
		Name:      fname,
		MemberIDs: []bson.ObjectID{},
	}
	if err := h.familyRepo.Create(r.Context(), family); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao alocar base tenant da sua Familia")
		return
	}

	// 4. Cria o usuário já vinculado na família recém nascida
	user := &models.User{
		Login:        req.Login,
		PasswordHash: string(hashed),
		FamilyID:     family.ID,
	}
	if err := h.userRepo.Create(r.Context(), user); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao alocar registro de usuário")
		return
	}

	// E cadastra ele como membro master dela
	_ = h.familyRepo.AddMember(r.Context(), family.ID, user.ID)

	// 5. Devolve Autenticado
	resp, err := h.issueTokenPair(r, user)
	if err != nil {
		response.JSON(w, http.StatusCreated, map[string]string{"message": "logado criado com sucesso, mas faça login para continuar"})
		return
	}

	response.JSON(w, http.StatusCreated, resp)
}

// Login handles POST /auth/login.
//
// @Summary      Login
// @Description  Autentica com login/senha e retorna um par de tokens (access token de curta duração + refresh token)
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      loginRequest   true  "Credenciais"
// @Success      200   {object}  loginResponse
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      429   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "invalid request body"})
		return
	}

	// Same generic error for "user not found" and "wrong password" to prevent
	// user enumeration — an attacker should never know which one failed.
	user, err := h.userRepo.FindByLogin(r.Context(), req.Login)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(errorResponse{Error: "invalid credentials"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "internal error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "invalid credentials"})
		return
	}

	resp, err := h.issueTokenPair(r, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "could not generate tokens"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// Refresh handles POST /auth/refresh.
//
// @Summary      Renovar tokens
// @Description  Troca um refresh token válido por um novo par access + refresh (rotação automática — o token antigo é invalidado)
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      refreshRequest  true  "Refresh token atual"
// @Success      200   {object}  loginResponse
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /auth/refresh [post]
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "refresh_token is required"})
		return
	}

	hash := h.service.HashToken(req.RefreshToken)
	stored, err := h.refreshRepo.FindByHash(r.Context(), hash)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "invalid or expired refresh token"})
		return
	}

	// Rotation: revoke the old token before issuing a new one.
	if err := h.refreshRepo.Revoke(r.Context(), stored.ID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "internal error"})
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), stored.UserID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "internal error"})
		return
	}

	resp, err := h.issueTokenPair(r, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "could not generate tokens"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// Logout handles POST /auth/logout.
//
// @Summary      Logout
// @Description  Adiciona o access token à blocklist e revoga o refresh token fornecido
// @Tags         auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  logoutRequest  false  "Refresh token a revogar (opcional)"
// @Success      204
// @Failure      500  {object}  errorResponse
// @Router       /auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	// Blocklist the current access token so it cannot be reused after logout.
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		if parts := strings.SplitN(authHeader, " ", 2); len(parts) == 2 {
			rawToken := parts[1]
			if claims, err := h.service.ValidateToken(rawToken); err == nil && claims.ExpiresAt != nil {
				tokenHash := h.service.HashToken(rawToken)
				_ = h.blocklistRepo.Add(r.Context(), tokenHash, claims.ExpiresAt.Time)
			}
		}
	}

	// Revoke the provided refresh token if supplied.
	var req logoutRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body is optional
	if req.RefreshToken != "" {
		hash := h.service.HashToken(req.RefreshToken)
		if stored, err := h.refreshRepo.FindByHash(r.Context(), hash); err == nil {
			_ = h.refreshRepo.Revoke(r.Context(), stored.ID)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// ChangePassword handles PUT /auth/password.
//
// @Summary      Trocar senha
// @Description  Troca a senha do usuário autenticado. Invalida todos os refresh tokens existentes (força re-login em todos os dispositivos).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  changePasswordRequest  true  "Senhas atual e nova"
// @Success      204
// @Failure      400  {object}  errorResponse
// @Failure      401  {object}  errorResponse
// @Failure      500  {object}  errorResponse
// @Router       /auth/password [put]
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Re-extract claims from the header (avoids importing middleware → no circular dep).
	// The Authenticate middleware already validated this token; re-parsing is cheap.
	claims := GetClaims(r)
	if claims == nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "unauthorized"})
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "invalid request body"})
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "current_password and new_password are required"})
		return
	}

	if len(req.NewPassword) < 8 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "new_password must be at least 8 characters"})
		return
	}

	userID, err := bson.ObjectIDFromHex(claims.UserID)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "invalid token claims"})
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "internal error"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "current password is incorrect"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "internal error"})
		return
	}

	if err := h.userRepo.UpdatePassword(r.Context(), userID, string(newHash)); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "internal error"})
		return
	}

	// Revoke all refresh tokens — forces re-login on all devices after password change.
	_ = h.refreshRepo.RevokeAllByUser(r.Context(), userID)

	w.WriteHeader(http.StatusNoContent)
}

// — Helper ─────────────────────────────────────────────────────────────────────

// issueTokenPair generates an access token + refresh token for the given user,
// persists the refresh token hash, and returns both to the caller.
func (h *Handler) issueTokenPair(r *http.Request, user *models.User) (loginResponse, error) {
	accessToken, err := h.service.GenerateToken(user)
	if err != nil {
		return loginResponse{}, err
	}

	rawRefresh, refreshHash, err := h.service.GenerateRefreshToken()
	if err != nil {
		return loginResponse{}, err
	}

	rt := &models.RefreshToken{
		ID:        bson.NewObjectID(),
		UserID:    user.ID,
		TokenHash: refreshHash,
		ExpiresAt: time.Now().Add(h.refreshExpiresIn),
		Revoked:   false,
	}
	if err := h.refreshRepo.Create(r.Context(), rt); err != nil {
		return loginResponse{}, err
	}

	return loginResponse{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
	}, nil
}
