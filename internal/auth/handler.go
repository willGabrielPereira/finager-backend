package auth

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	Login       string `json:"login"        validate:"required,min=4,max=32"`
	Email       string `json:"email"        validate:"required,email"`
	Password    string `json:"password"     validate:"required,min=8,max=72"`
	FamilyName  string `json:"family_name"  validate:"omitempty,min=2"`
	InviteToken string `json:"invite_token" validate:"omitempty"`
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

type elevateRequest struct {
	Password string `json:"password"`
}

// errorResponse is returned on any failure.
type errorResponse struct {
	Error string `json:"error"`
}

// — Handler ────────────────────────────────────────────────────────────────────

// rateLimiter é o subconjunto de middleware.RateLimiter que o Handler precisa.
// Definido aqui (em vez de importar internal/middleware) porque middleware já
// importa internal/auth — importar middleware de volta criaria um ciclo.
// middleware.InMemoryRateLimiter satisfaz esta interface estruturalmente.
type rateLimiter interface {
	Allow(key string) bool
}

// Handler holds dependencies needed by auth HTTP handlers.
type Handler struct {
	service          *Service
	userRepo         *repository.UserRepository
	familyRepo       *repository.FamilyRepository
	refreshRepo      *repository.RefreshTokenRepository
	blocklistRepo    *repository.BlocklistRepository
	inviteRepo       *repository.FamilyInviteRepository
	signupInviteRepo *repository.SignupInviteRepository
	refreshExpiresIn time.Duration
	elevateLimiter   rateLimiter
}

// NewHandler creates an auth Handler with all required dependencies.
func NewHandler(
	svc *Service,
	userRepo *repository.UserRepository,
	familyRepo *repository.FamilyRepository,
	refreshRepo *repository.RefreshTokenRepository,
	blocklistRepo *repository.BlocklistRepository,
	inviteRepo *repository.FamilyInviteRepository,
	signupInviteRepo *repository.SignupInviteRepository,
	refreshExpiresHours int,
	elevateLimiter rateLimiter,
) *Handler {
	return &Handler{
		service:          svc,
		userRepo:         userRepo,
		familyRepo:       familyRepo,
		refreshRepo:      refreshRepo,
		blocklistRepo:    blocklistRepo,
		inviteRepo:       inviteRepo,
		signupInviteRepo: signupInviteRepo,
		refreshExpiresIn: time.Duration(refreshExpiresHours) * time.Hour,
		elevateLimiter:   elevateLimiter,
	}
}

// — Endpoints ──────────────────────────────────────────────────────────────────

// Register handles POST /auth/register
// @Summary      Cadastro de Usuário
// @Description  Cadastra um novo usuário e automaticamente instancializa uma Familia root vinculada a ele, ou vincula à família do convite.
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

	req.Login = strings.TrimSpace(req.Login)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	// Valida campos
	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	// 1. Checa unicidade do login
	existsLogin, _ := h.userRepo.FindByLogin(r.Context(), req.Login)
	if existsLogin != nil {
		response.Validations(w, response.ValidationError{Field: "login", Rule: "unique", Message: "Este nome de usuário já está sendo utilizado"})
		return
	}

	// 2. Checa unicidade do e-mail
	existsEmail, _ := h.userRepo.FindByEmail(r.Context(), req.Email)
	if existsEmail != nil {
		response.Validations(w, response.ValidationError{Field: "email", Rule: "unique", Message: "Este e-mail já está cadastrado em outra conta"})
		return
	}

	// 3. Verifica convite se fornecido.
	// O campo invite_token é compartilhado por dois tipos de convite:
	//   - FamilyInvite: convite para ENTRAR em uma família já existente (comportamento
	//     original, mantido idêntico abaixo).
	//   - SignupInvite: convite administrativo que CONCEDE um plano (ex.: LIFETIME_FREE)
	//     à família NOVA criada neste cadastro.
	// Tenta primeiro como convite de família; se o token não corresponder a nenhum,
	// tenta como convite de cadastro.
	//
	// Para o SignupInvite, a reivindicação (ClaimByToken) acontece AQUI — antes de criar
	// qualquer família/usuário — em vez de só no fim do fluxo. Isso fecha dois bugs que a
	// ordem antiga (criar família + usuário, marcar convite usado só no final) permitia:
	//   (a) fail-open: se marcar-como-usado falhasse por um motivo transitório (timeout,
	//       erro de conexão) no final, o cadastro já tinha sido concluído mesmo assim e o
	//       token continuava reutilizável indefinidamente;
	//   (b) conta órfã: em duas requisições concorrentes com o mesmo token, a que perdesse
	//       a corrida só descobriria isso DEPOIS de já ter criado usuário e família no banco.
	// Reivindicando primeiro (com uma condição atômica no UPDATE), nenhuma linha de
	// usuário/família é criada antes de sabermos que o convite é legitimamente nosso.
	var targetFamilyID uuid.UUID
	var invite *models.FamilyInvite
	var signupInvite *models.SignupInvite
	if req.InviteToken != "" {
		inv, err := h.inviteRepo.FindByToken(r.Context(), req.InviteToken)
		if err == nil && inv != nil {
			if inv.UsedAt != nil {
				response.Validations(w, response.ValidationError{Field: "invite_token", Rule: "used", Message: "Este convite já foi utilizado"})
				return
			}
			if inv.ExpiresAt.Before(time.Now()) {
				response.Validations(w, response.ValidationError{Field: "invite_token", Rule: "expired", Message: "Este convite expirou"})
				return
			}
			if inv.TargetEmail != nil && *inv.TargetEmail != "" && !strings.EqualFold(*inv.TargetEmail, req.Email) {
				response.Validations(w, response.ValidationError{
					Field:   "email",
					Rule:    "mismatch",
					Message: "Este convite foi emitido exclusivamente para o e-mail: " + *inv.TargetEmail,
				})
				return
			}
			invite = inv
			targetFamilyID = inv.FamilyID
		} else {
			claimed, claimErr := h.signupInviteRepo.ClaimByToken(r.Context(), req.InviteToken)
			if claimErr != nil {
				response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha interna ao validar convite de cadastro")
				return
			}
			if claimed == nil {
				// Não conseguiu reivindicar (token inexistente, já usado ou expirado):
				// nenhuma linha de usuário/família foi tocada. Busca o convite de novo só
				// para determinar qual das três mensagens específicas devolver ao cliente.
				si, siErr := h.signupInviteRepo.GetByToken(r.Context(), req.InviteToken)
				if siErr != nil || si == nil {
					response.Validations(w, response.ValidationError{Field: "invite_token", Rule: "invalid", Message: "Código de convite não encontrado ou inválido"})
					return
				}
				if si.UsedAt != nil {
					response.Validations(w, response.ValidationError{Field: "invite_token", Rule: "used", Message: "Este convite já foi utilizado"})
					return
				}
				if time.Now().After(si.ExpiresAt) {
					response.Validations(w, response.ValidationError{Field: "invite_token", Rule: "expired", Message: "Este convite expirou"})
					return
				}
				// Estado mudou entre o claim e esta checagem (ex.: outra requisição
				// reivindicou no meio-tempo): trata como inválido por segurança.
				response.Validations(w, response.ValidationError{Field: "invite_token", Rule: "invalid", Message: "Código de convite não encontrado ou inválido"})
				return
			}
			signupInvite = claimed
		}
	}

	// 4. Hash Password
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha interna ao gerar hash de senha")
		return
	}

	// 5. Família: se não veio por convite de família, cria uma nova família
	// (com o plano concedido pelo convite de cadastro, se houver; senão o padrão FREE).
	if invite == nil {
		fname := req.FamilyName
		if fname == "" {
			fname = "Família de " + req.Login
		}

		family := &models.Family{
			Name:      fname,
			MemberIDs: []uuid.UUID{},
		}
		if signupInvite != nil {
			family.Plan = signupInvite.PlanGranted
		}
		if err := h.familyRepo.Create(r.Context(), family); err != nil {
			// O convite de cadastro (se houver) já foi reivindicado no passo 3, antes de
			// chegarmos aqui. Se a criação da família falhar agora por um motivo
			// transitório não relacionado ao convite, devolve a reivindicação — senão um
			// convite válido seria desperdiçado por um erro que nada tem a ver com ele.
			if signupInvite != nil {
				if relErr := h.signupInviteRepo.ReleaseClaim(r.Context(), signupInvite.ID); relErr != nil {
					log.Printf("Aviso: falha ao liberar reivindicação do convite de cadastro %s após erro na criação da família: %v", signupInvite.ID, relErr)
				}
			}
			response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao alocar espaço familiar")
			return
		}
		targetFamilyID = family.ID
	}

	// 6. Cria o usuário vinculado à família
	user := &models.User{
		Login:        req.Login,
		Email:        req.Email,
		PasswordHash: string(hashed),
		FamilyID:     targetFamilyID,
	}
	if err := h.userRepo.Create(r.Context(), user); err != nil {
		// Mesmo raciocínio do passo 5: libera a reivindicação antes de devolver o erro,
		// para não desperdiçar um convite válido por uma falha transitória na criação do usuário.
		if signupInvite != nil {
			if relErr := h.signupInviteRepo.ReleaseClaim(r.Context(), signupInvite.ID); relErr != nil {
				log.Printf("Aviso: falha ao liberar reivindicação do convite de cadastro %s após erro na criação do usuário: %v", signupInvite.ID, relErr)
			}
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao alocar registro de usuário: "+err.Error())
		return
	}

	// 7. Cadastra ele como membro da família
	_ = h.familyRepo.AddMember(r.Context(), targetFamilyID, user.ID)

	// 8. Se usou convite de família, marca como utilizado
	if invite != nil {
		_ = h.inviteRepo.MarkAsUsed(r.Context(), invite.ID, user.ID)
	}
	// Se usou convite de cadastro, a reivindicação de segurança já aconteceu no
	// ClaimByToken (passo 3) — o token já está marcado como usado e não pode ser
	// reaproveitado. Aqui só registramos qual família o usou, para auditoria; se isso
	// falhar, é apenas cosmético (não deixa o convite reutilizável), então só logamos.
	if signupInvite != nil {
		if err := h.signupInviteRepo.SetUsedFamily(r.Context(), signupInvite.ID, targetFamilyID); err != nil {
			log.Printf("Aviso: falha ao registrar família %s no convite de cadastro %s: %v", targetFamilyID, signupInvite.ID, err)
		}
	}

	// 9. Devolve Autenticado
	resp, err := h.issueTokenPair(r, user)
	if err != nil {
		response.JSON(w, http.StatusCreated, map[string]string{"message": "Conta criada com sucesso, por favor faça login."})
		return
	}

	response.JSON(w, http.StatusCreated, resp)
}

// Login handles POST /auth/login.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "invalid request body"})
		return
	}

	req.Login = strings.TrimSpace(req.Login)

	// Busca por login ou e-mail
	user, err := h.userRepo.FindByLoginOrEmail(r.Context(), req.Login)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(errorResponse{Error: "Credenciais inválidas"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "Erro interno"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "Credenciais inválidas"})
		return
	}

	resp, err := h.issueTokenPair(r, user)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "Não foi possível gerar tokens de autenticação"})
		return
	}

	// Registra o último login. Não-fatal: falhar aqui não deve impedir o login.
	if err := h.userRepo.UpdateLastLogin(r.Context(), user.ID); err != nil {
		log.Printf("Aviso: falha ao atualizar last_login_at do usuário %s: %v", user.ID, err)
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

	// Se o cliente também tinha um token elevado (step-up admin) ativo, blocklista-o
	// também — mesmo padrão acima. Header ausente: nenhuma mudança de comportamento
	// para quem nunca usou a área admin.
	if elevatedToken := r.Header.Get("X-Admin-Elevation"); elevatedToken != "" {
		if claims, err := h.service.ValidateToken(elevatedToken); err == nil && claims.ExpiresAt != nil {
			tokenHash := h.service.HashToken(elevatedToken)
			_ = h.blocklistRepo.Add(r.Context(), tokenHash, claims.ExpiresAt.Time)
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

	userID, err := uuid.Parse(claims.UserID)
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

// Elevate handles POST /auth/elevate — step-up auth: reautentica com a senha atual
// e emite um token de curta duração (audiência "admin") exigido pelas rotas /admin/*
// além do papel do usuário. Não concede papel nenhum, só prova identidade recente.
//
// @Summary      Reautenticação step-up
// @Description  Reautentica o usuário autenticado com a senha atual e emite um token elevado de curta duração (15 min, audiência "admin") exigido pelas rotas administrativas.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  elevateRequest  true  "Senha atual"
// @Success      200   {object}  map[string]interface{}
// @Failure      400   {object}  errorResponse
// @Failure      401   {object}  errorResponse
// @Failure      403   {object}  errorResponse
// @Failure      429   {object}  errorResponse
// @Failure      500   {object}  errorResponse
// @Router       /auth/elevate [post]
func (h *Handler) Elevate(w http.ResponseWriter, r *http.Request) {
	claims := GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "não autenticado")
		return
	}

	var req elevateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "payload inválido")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "não autenticado")
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "erro interno")
		return
	}

	// Papel insuficiente: retorna ANTES do rate limit e do bcrypt — não vale gastar
	// tentativa de rate limit nem o custo/timing do bcrypt em quem nunca teria acesso.
	// Revelar isso pro PRÓPRIO usuário autenticado é seguro (ele já provou identidade
	// via o access token); não serve de oráculo pra descobrir logins de terceiros.
	if user.Role != "admin" && user.Role != "moderator" {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN_ROLE", "papel insuficiente")
		return
	}

	if !h.elevateLimiter.Allow(claims.UserID) {
		response.Error(w, http.StatusTooManyRequests, "E_RATE_LIMITED", "muitas tentativas, aguarde")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		// 403, não 401: o frontend trata 401 como sessão expirada e dispara rotação de
		// refresh token, o que não queremos numa simples senha errada de step-up.
		response.Error(w, http.StatusForbidden, "E_INVALID_PASSWORD", "senha incorreta")
		return
	}

	token, err := h.service.GenerateElevatedToken(user)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "falha ao gerar token elevado")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"elevated_token": token,
		"expires_at":     time.Now().Add(15 * time.Minute).Format(time.RFC3339),
	})
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
		ID:        uuid.New(),
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
