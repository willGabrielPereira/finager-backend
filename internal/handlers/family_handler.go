package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
	"github.com/willGabrielPereira/finager-backend/pkg/validator"
)

type FamilyHandler struct {
	familyRepo *repository.FamilyRepository
	inviteRepo *repository.FamilyInviteRepository
	userRepo   *repository.UserRepository
}

func NewFamilyHandler(
	familyRepo *repository.FamilyRepository,
	inviteRepo *repository.FamilyInviteRepository,
	userRepo *repository.UserRepository,
) *FamilyHandler {
	return &FamilyHandler{
		familyRepo: familyRepo,
		inviteRepo: inviteRepo,
		userRepo:   userRepo,
	}
}

// GetMembers returns all members belonging to the authenticated user's family.
// GET /family/members
func (h *FamilyHandler) GetMembers(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Família inválida no token")
		return
	}

	members, err := h.familyRepo.GetMembers(r.Context(), familyID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao buscar membros da família")
		return
	}

	response.JSON(w, http.StatusOK, members)
}

type createInviteRequest struct {
	TargetEmail string `json:"target_email" validate:"omitempty,email"`
}

type inviteResponse struct {
	ID          uuid.UUID `json:"id"`
	Token       string    `json:"token"`
	TargetEmail *string   `json:"target_email,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateInvite generates a cryptographically secure 256-bit token for family invitation.
// POST /family/invites
func (h *FamilyHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Família inválida no token")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Usuário inválido no token")
		return
	}

	var req createInviteRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	var targetEmail *string
	if strings.TrimSpace(req.TargetEmail) != "" {
		cleaned := strings.ToLower(strings.TrimSpace(req.TargetEmail))
		targetEmail = &cleaned
	}

	invite := &models.FamilyInvite{
		FamilyID:    familyID,
		TargetEmail: targetEmail,
		CreatedBy:   userID,
		ExpiresAt:   time.Now().Add(48 * time.Hour), // Expira em 48h
	}

	if err := h.inviteRepo.Create(r.Context(), invite); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao gerar convite seguro: "+err.Error())
		return
	}

	response.JSON(w, http.StatusCreated, inviteResponse{
		ID:          invite.ID,
		Token:       invite.Token,
		TargetEmail: invite.TargetEmail,
		ExpiresAt:   invite.ExpiresAt,
		CreatedAt:   invite.CreatedAt,
	})
}

// ValidateInvite verifies if an invite token is valid and returns family name and target email if any.
// GET /family/invites/validate?token=...
func (h *FamilyHandler) ValidateInvite(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Token não fornecido")
		return
	}

	inv, err := h.inviteRepo.FindByToken(r.Context(), token)
	if err != nil || inv == nil {
		response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Convite inexistente ou inválido")
		return
	}

	if inv.UsedAt != nil {
		response.Error(w, http.StatusGone, "E_USED", "Este convite já foi utilizado")
		return
	}

	if inv.ExpiresAt.Before(time.Now()) {
		response.Error(w, http.StatusGone, "E_EXPIRED", "Este convite expirou")
		return
	}

	family, err := h.familyRepo.FindByID(r.Context(), inv.FamilyID)
	if err != nil || family == nil {
		response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Família associada ao convite não foi encontrada")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"valid":        true,
		"family_name":  family.Name,
		"target_email": inv.TargetEmail,
		"expires_at":   inv.ExpiresAt,
	})
}

type joinFamilyRequest struct {
	Token string `json:"token" validate:"required"`
}

// Join allows an authenticated user to join a family using a valid invite token.
// POST /family/join
func (h *FamilyHandler) Join(w http.ResponseWriter, r *http.Request) {
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

	var req joinFamilyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Payload inválido")
		return
	}

	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	inv, err := h.inviteRepo.FindByToken(r.Context(), strings.TrimSpace(req.Token))
	if err != nil || inv == nil {
		response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Convite inexistente ou inválido")
		return
	}

	if inv.UsedAt != nil {
		response.Error(w, http.StatusGone, "E_USED", "Este convite já foi utilizado")
		return
	}

	if inv.ExpiresAt.Before(time.Now()) {
		response.Error(w, http.StatusGone, "E_EXPIRED", "Este convite expirou")
		return
	}

	if inv.TargetEmail != nil && *inv.TargetEmail != "" && !strings.EqualFold(*inv.TargetEmail, user.Email) {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Este convite foi emitido para outro endereço de e-mail ("+*inv.TargetEmail+")")
		return
	}

	// Adiciona aos membros da família e atualiza a família ativa do usuário
	if err := h.familyRepo.AddMember(r.Context(), inv.FamilyID, user.ID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao vincular membro na família")
		return
	}

	if err := h.userRepo.UpdateFamilyID(r.Context(), user.ID, inv.FamilyID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao atualizar família ativa do usuário")
		return
	}

	_ = h.inviteRepo.MarkAsUsed(r.Context(), inv.ID, user.ID)

	family, _ := h.familyRepo.FindByID(r.Context(), inv.FamilyID)
	familyName := ""
	if family != nil {
		familyName = family.Name
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message":     "Você ingressou com sucesso na família!",
		"family_id":   inv.FamilyID,
		"family_name": familyName,
	})
}

// RemoveMember removes a member from the family.
// DELETE /family/members/{id}
func (h *FamilyHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Família inválida no token")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Usuário inválido no token")
		return
	}

	memberIDStr := r.PathValue("id")
	memberID, err := uuid.Parse(memberIDStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_ID", "ID de membro inválido")
		return
	}

	if memberID == userID {
		response.Error(w, http.StatusBadRequest, "E_BAD_REQUEST", "Você não pode remover a si mesmo da família ativa por esta rota")
		return
	}

	if err := h.familyRepo.RemoveMember(r.Context(), familyID, memberID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao remover membro da família")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "Membro removido da família com sucesso"})
}
