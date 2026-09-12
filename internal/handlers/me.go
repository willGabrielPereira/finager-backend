package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

type ProfileResponse struct {
	UserID     string    `json:"user_id"`
	Login      string    `json:"login"`
	FamilyID   string    `json:"family_id"`
	FamilyName string    `json:"family_name"`
	CreatedAt  time.Time `json:"created_at"`
}

type updateProfileRequest struct {
	Login      string `json:"login"`
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
	if family, err := h.familyRepo.FindByID(r.Context(), user.FamilyID); err == nil && family != nil {
		familyName = family.Name
	}

	response.JSON(w, http.StatusOK, ProfileResponse{
		UserID:     user.ID.String(),
		Login:      user.Login,
		FamilyID:   user.FamilyID.String(),
		FamilyName: familyName,
		CreatedAt:  user.CreatedAt,
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

	newFamilyName := strings.TrimSpace(req.FamilyName)
	if newFamilyName != "" {
		if len(newFamilyName) < 2 {
			response.Error(w, http.StatusUnprocessableEntity, "E_VALIDATION", "O nome da família deve ter pelo menos 2 caracteres")
			return
		}
		_ = h.familyRepo.UpdateName(r.Context(), user.FamilyID, newFamilyName)
	}

	familyName := newFamilyName
	if familyName == "" {
		if family, err := h.familyRepo.FindByID(r.Context(), user.FamilyID); err == nil && family != nil {
			familyName = family.Name
		}
	}

	response.JSON(w, http.StatusOK, ProfileResponse{
		UserID:     user.ID.String(),
		Login:      user.Login,
		FamilyID:   user.FamilyID.String(),
		FamilyName: familyName,
		CreatedAt:  user.CreatedAt,
	})
}

