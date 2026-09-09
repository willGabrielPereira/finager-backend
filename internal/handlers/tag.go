package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
	"github.com/willGabrielPereira/finager-backend/pkg/validator"
)

type TagHandler struct {
	tagRepo *repository.TagRepository
}

func NewTagHandler(tagRepo *repository.TagRepository) *TagHandler {
	return &TagHandler{tagRepo: tagRepo}
}

// createTagRequest DTO
type createTagRequest struct {
	Name  string `json:"name"  validate:"required,min=2"`
	Color string `json:"color" validate:"required,hexcolor"`
	Icon  string `json:"icon"  validate:"required"`
}

// updateTagRequest DTO
type updateTagRequest struct {
	Name  string `json:"name"  validate:"omitempty,min=2"`
	Color string `json:"color" validate:"omitempty,hexcolor"`
	Icon  string `json:"icon"  validate:"omitempty"`
}

// List
// @Summary      Listar Tags
// @Description  Retorna todas as tags visíveis (Globais do sistema + Customizadas da Família)
// @Router       /tags [get]
func (h *TagHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "invalid family in token")
		return
	}

	tags, err := h.tagRepo.FindAllVisible(r.Context(), familyID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao ler conjunto de tags")
		return
	}

	response.JSON(w, http.StatusOK, tags)
}

// Create
// @Summary      Criar Tag
// @Description  Aloca uma tag customizada associando-a a Família do usuário
// @Router       /tags [post]
func (h *TagHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	var req createTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload inválido"})
		return
	}

	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	familyID, _ := uuid.Parse(claims.FamilyID)

	tag := &models.Tag{
		Name:     req.Name,
		Color:    req.Color,
		Icon:     req.Icon,
		FamilyID: &familyID,
		IsSystem: false, // Força a ser False pra proteção
	}

	if err := h.tagRepo.Create(r.Context(), tag); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao alocar Tag")
		return
	}

	response.JSON(w, http.StatusCreated, tag)
}

// Update
// @Summary      Atualizar Tag
// @Description  Altera livremente Color, Nome e Icon de tags personalizadas.
// @Router       /tags/{id} [put]
func (h *TagHandler) Update(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	tagIDHex := r.PathValue("id")
	tagID, err := uuid.Parse(tagIDHex)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_VALIDATION", "O ID informado via URL é inoperável")
		return
	}

	familyID, _ := uuid.Parse(claims.FamilyID)

	tag, err := h.tagRepo.FindByID(r.Context(), tagID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "A tag solicitada não foi localizada")
			return
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha interna")
		return
	}

	// Segurança RLS e Bloqueios
	if tag.IsSystem {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "É proibido modificar Tags Nativas (Globais) de Sistema. Elas são gerenciadas de forma centralizada.")
		return
	}
	if tag.FamilyID == nil || *tag.FamilyID != familyID {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Você não tem permissão em recursos desta família alheia.")
		return
	}

	var req updateTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload de update inválido"})
		return
	}
	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Color != "" {
		updates["color"] = req.Color
	}
	if req.Icon != "" {
		updates["icon"] = req.Icon
	}

	if len(updates) > 0 {
		if err := h.tagRepo.Update(r.Context(), tagID, updates); err != nil {
			response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Houve uma falha ao modificar seu registro no BD")
			return
		}
	}

	// 204 No Content foi requisitado pelo usuário! 
	w.WriteHeader(http.StatusNoContent)
}

// Delete
// @Summary      Deletar Tag
// @Router       /tags/{id} [delete]
func (h *TagHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	tagIDHex := r.PathValue("id")
	tagID, err := uuid.Parse(tagIDHex)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_VALIDATION", "O ID informado via URL é inoperável")
		return
	}

	familyID, _ := uuid.Parse(claims.FamilyID)

	tag, err := h.tagRepo.FindByID(r.Context(), tagID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "A tag solicitada não foi encontrada pra ser deletada")
			return
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Ocorreu um erro interno")
		return
	}

	// Impedimentos
	if tag.IsSystem {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Tags de sistema são fixas e nativas do Produto. Impossíveis de serem apagadas.")
		return
	}
	if tag.FamilyID == nil || *tag.FamilyID != familyID {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Proibido. Escopo fora da família.")
		return
	}

	if err := h.tagRepo.Delete(r.Context(), tagID, familyID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha de execução")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
