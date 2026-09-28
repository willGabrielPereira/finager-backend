package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

type ProfileResponse struct {
	UserID              string    `json:"user_id"`
	Login               string    `json:"login"`
	Email               string    `json:"email"`
	FamilyID            string    `json:"family_id"`
	FamilyName          string    `json:"family_name"`
	Plan                string    `json:"plan"`
	SubscriptionStatus  string    `json:"subscription_status"`
	OnboardingCompleted bool      `json:"onboarding_completed"`
	OnboardingStep      int       `json:"onboarding_step"`
	Role                string    `json:"role"`
	CreatedAt           time.Time `json:"created_at"`
}

type updateProfileRequest struct {
	Login      string `json:"login"`
	Email      string `json:"email"`
	FamilyName string `json:"family_name"`
}

// ProfileHandler gerencia consulta e atualização dos dados cadastrais do usuário e sua família.
type ProfileHandler struct {
	userRepo   *repository.UserRepository
	familyRepo *repository.FamilyRepository
}

// NewProfileHandler instancia o ProfileHandler com os repositórios necessários.
func NewProfileHandler(userRepo *repository.UserRepository, familyRepo *repository.FamilyRepository) *ProfileHandler {
	return &ProfileHandler{
		userRepo:   userRepo,
		familyRepo: familyRepo,
	}
}

// Get retorna o perfil do usuário autenticado e dados da sua família.
// @Summary      Perfil do usuário
// @Tags         profile
// @Router       /me [get]
func (h *ProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil || user == nil {
		response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Usuário não encontrado")
		return
	}

	familyName := ""
	plan := "FREE"
	subStatus := "ACTIVE"
	if family, err := h.familyRepo.FindByID(r.Context(), user.FamilyID); err == nil && family != nil {
		familyName = family.Name
		plan = family.Plan
		subStatus = family.SubscriptionStatus
	}

	response.JSON(w, http.StatusOK, ProfileResponse{
		UserID:              user.ID.String(),
		Login:               user.Login,
		Email:               user.Email,
		FamilyID:            user.FamilyID.String(),
		FamilyName:          familyName,
		Plan:                plan,
		SubscriptionStatus:  subStatus,
		OnboardingCompleted: user.OnboardingCompleted,
		OnboardingStep:      user.OnboardingStep,
		Role:                user.Role,
		CreatedAt:           user.CreatedAt,
	})
}

// Update atualiza nome de usuário e/ou nome da família.
// @Summary      Atualizar perfil
// @Tags         profile
// @Router       /me [put]
func (h *ProfileHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil || user == nil {
		response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Usuário não encontrado")
		return
	}

	var req updateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Payload inválido")
		return
	}

	newLogin := strings.TrimSpace(req.Login)
	if newLogin != "" && newLogin != user.Login {
		if len(newLogin) < 4 {
			response.Error(w, http.StatusUnprocessableEntity, "E_VALIDATION", "O login deve ter pelo menos 4 caracteres")
			return
		}
		existing, _ := h.userRepo.FindByLogin(r.Context(), newLogin)
		if existing != nil && existing.ID != user.ID {
			response.Error(w, http.StatusConflict, "E_CONFLICT", "Este login já está em uso por outro usuário")
			return
		}
		if err := h.userRepo.UpdateLogin(r.Context(), user.ID, newLogin); err != nil {
			response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao atualizar login")
			return
		}
		user.Login = newLogin
	}

	newEmail := strings.ToLower(strings.TrimSpace(req.Email))
	if newEmail != "" && newEmail != strings.ToLower(user.Email) {
		if !strings.Contains(newEmail, "@") || !strings.Contains(newEmail, ".") {
			response.Error(w, http.StatusUnprocessableEntity, "E_VALIDATION", "Por favor informe um e-mail válido")
			return
		}
		existing, _ := h.userRepo.FindByEmail(r.Context(), newEmail)
		if existing != nil && existing.ID != user.ID {
			response.Error(w, http.StatusConflict, "E_CONFLICT", "Este e-mail já está em uso por outro usuário")
			return
		}
		if err := h.userRepo.UpdateEmail(r.Context(), user.ID, newEmail); err != nil {
			response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao atualizar e-mail")
			return
		}
		user.Email = newEmail
	}

	newFamilyName := strings.TrimSpace(req.FamilyName)
	if newFamilyName != "" {
		if len(newFamilyName) < 2 {
			response.Error(w, http.StatusUnprocessableEntity, "E_VALIDATION", "O nome da família deve ter pelo menos 2 caracteres")
			return
		}
		_ = h.familyRepo.UpdateName(r.Context(), user.FamilyID, newFamilyName)
	}

	familyName := newFamilyName
	plan := "FREE"
	subStatus := "ACTIVE"
	if family, err := h.familyRepo.FindByID(r.Context(), user.FamilyID); err == nil && family != nil {
		if familyName == "" {
			familyName = family.Name
		}
		plan = family.Plan
		subStatus = family.SubscriptionStatus
	}

	response.JSON(w, http.StatusOK, ProfileResponse{
		UserID:              user.ID.String(),
		Login:               user.Login,
		Email:               user.Email,
		FamilyID:            user.FamilyID.String(),
		FamilyName:          familyName,
		Plan:                plan,
		SubscriptionStatus:  subStatus,
		OnboardingCompleted: user.OnboardingCompleted,
		OnboardingStep:      user.OnboardingStep,
		Role:                user.Role,
		CreatedAt:           user.CreatedAt,
	})
}

type updateOnboardingRequest struct {
	Completed bool `json:"completed"`
	Step      int  `json:"step"`
}

// UpdateOnboarding atualiza o progresso do tutorial/onboarding do usuário.
// @Summary      Atualizar progresso de onboarding
// @Tags         profile
// @Router       /me/onboarding [patch]
func (h *ProfileHandler) UpdateOnboarding(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	var req updateOnboardingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Payload inválido")
		return
	}

	if err := h.userRepo.UpdateOnboarding(r.Context(), userID, req.Completed, req.Step); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao atualizar onboarding")
		return
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"onboarding_completed": req.Completed,
		"onboarding_step":      req.Step,
		"message":              "Progresso de onboarding atualizado com sucesso",
	})
}

type deleteAccountRequest struct {
	Password string `json:"password"`
}

// Delete remove a conta do usuário e aplica o direito à eliminação da LGPD.
// @Summary      Excluir conta (LGPD)
// @Tags         profile
// @Router       /me [delete]
func (h *ProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil || user == nil {
		response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Usuário não encontrado")
		return
	}

	var req deleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Informe sua senha atual para confirmar a exclusão")
		return
	}

	if strings.TrimSpace(req.Password) == "" {
		response.Error(w, http.StatusUnprocessableEntity, "E_VALIDATION", "Por favor informe sua senha para confirmar a exclusão")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_PASSWORD", "Senha incorreta. A exclusão foi cancelada por segurança")
		return
	}

	if err := h.userRepo.DeleteAccount(r.Context(), userID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao excluir conta: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{
		"message": "Sua conta e todos os dados associados foram excluídos com sucesso em conformidade com a LGPD.",
	})
}


