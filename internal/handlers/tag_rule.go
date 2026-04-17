package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
	"github.com/willGabrielPereira/finager-backend/pkg/validator"
)

// TagRuleHandler agrupa os handlers de regras de auto-tagging por nome/memo.
type TagRuleHandler struct {
	ruleRepo *repository.TagRuleRepository
}

// NewTagRuleHandler cria um TagRuleHandler com o repositório injetado.
func NewTagRuleHandler(ruleRepo *repository.TagRuleRepository) *TagRuleHandler {
	return &TagRuleHandler{ruleRepo: ruleRepo}
}

// ── DTOs ────────────────────────────────────────────────────────────────────────────

// createTagRuleRequest: tags enviadas como hex strings (ObjectIDs das tags).
type createTagRuleRequest struct {
	Pattern string   `json:"pattern" validate:"required,min=2"`
	Tags    []string `json:"tags"    validate:"required,min=1"`
}

// updateTagRuleRequest: ambos os campos opcionais.
type updateTagRuleRequest struct {
	Pattern string   `json:"pattern" validate:"omitempty,min=2"`
	Tags    []string `json:"tags"    validate:"omitempty"`
}

// parseTagIDs converte um slice de hex strings em []bson.ObjectID.
// Retorna erro descrevendo qual entrada é inválida.
func parseTagIDs(hexIDs []string) ([]bson.ObjectID, error) {
	ids := make([]bson.ObjectID, 0, len(hexIDs))
	for _, h := range hexIDs {
		id, err := bson.ObjectIDFromHex(h)
		if err != nil {
			return nil, fmt.Errorf("ID de tag inválido: %q", h)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// List retorna todas as regras visíveis: sistema + família autenticada.
//
// @Summary      Listar regras de auto-tagging
// @Description  Retorna regras globais de sistema e regras customizadas da família do usuário.
// @Tags         tag-rules
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   models.TagRule
// @Failure      401  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /tag-rules [get]
func (h *TagRuleHandler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := bson.ObjectIDFromHex(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Família inválida no token")
		return
	}

	rules, err := h.ruleRepo.FindAllVisible(r.Context(), familyID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao buscar regras de tagging")
		return
	}

	response.JSON(w, http.StatusOK, rules)
}

// Create cria uma nova regra de auto-tagging para a família do usuário autenticado.
//
// @Summary      Criar regra de auto-tagging
// @Description  Cria uma regra que mapeia um padrão (substring de name/memo) para um conjunto de tags. Sobrescreve regras de sistema com o mesmo padrão na hora da importação.
// @Tags         tag-rules
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      createTagRuleRequest  true  "Dados da regra"
// @Success      201   {object}  models.TagRule
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      422   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /tag-rules [post]
func (h *TagRuleHandler) Create(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	var req createTagRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload JSON inválido"})
		return
	}
	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	familyID, _ := bson.ObjectIDFromHex(claims.FamilyID)
	userID, _ := bson.ObjectIDFromHex(claims.UserID)
	now := time.Now().UTC()

	tagIDs, err := parseTagIDs(req.Tags)
	if err != nil {
		response.Validations(w, response.ValidationError{Field: "tags", Rule: "objectid", Message: err.Error()})
		return
	}

	rule := &models.TagRule{
		Pattern:   req.Pattern,
		Tags:      tagIDs,
		IsSystem:  false,
		FamilyID:  &familyID,
		CreatedBy: &userID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.ruleRepo.Create(r.Context(), rule); err != nil {
		// Conflito de padrão único para a família
		if mongo.IsDuplicateKeyError(err) {
			response.Error(w, http.StatusConflict, "E_CONFLICT", "Já existe uma regra com este padrão para sua família")
			return
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao criar regra de tagging")
		return
	}

	response.JSON(w, http.StatusCreated, rule)
}

// Update altera o padrão e/ou as tags de uma regra da família.
//
// @Summary      Atualizar regra de auto-tagging
// @Description  Atualiza padrão e/ou tags de uma regra customizada da família. Regras de sistema não podem ser modificadas diretamente — crie uma regra de família com o mesmo padrão para sobrescrevê-las.
// @Tags         tag-rules
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  string                true  "ID da regra"
// @Param        body  body  updateTagRuleRequest  true  "Campos a atualizar"
// @Success      204
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /tag-rules/{id} [put]
func (h *TagRuleHandler) Update(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	ruleID, err := bson.ObjectIDFromHex(r.PathValue("id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_VALIDATION", "ID de regra inválido")
		return
	}

	familyID, _ := bson.ObjectIDFromHex(claims.FamilyID)

	rule, err := h.ruleRepo.FindByID(r.Context(), ruleID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Regra não encontrada")
			return
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha interna")
		return
	}

	// Proteções RLS
	if rule.IsSystem {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN",
			"Regras de sistema não podem ser editadas diretamente. Crie uma regra de família com o mesmo padrão para sobrescrevê-la.")
		return
	}
	if rule.FamilyID == nil || *rule.FamilyID != familyID {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Esta regra não pertence à sua família")
		return
	}

	var req updateTagRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload JSON inválido"})
		return
	}
	if errs := validator.Struct(req); errs != nil {
		response.Validations(w, errs...)
		return
	}

	updates := bson.M{}
	if req.Pattern != "" {
		updates["pattern"] = req.Pattern
	}
	if len(req.Tags) > 0 {
		tagIDs, err := parseTagIDs(req.Tags)
		if err != nil {
			response.Validations(w, response.ValidationError{Field: "tags", Rule: "objectid", Message: err.Error()})
			return
		}
		updates["tags"] = tagIDs
	}

	if len(updates) > 0 {
		if err := h.ruleRepo.Update(r.Context(), ruleID, updates); err != nil {
			response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao atualizar regra")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// Delete remove uma regra de família.
//
// @Summary      Deletar regra de auto-tagging
// @Description  Remove uma regra customizada da família. Regras de sistema não podem ser removidas.
// @Tags         tag-rules
// @Security     BearerAuth
// @Param        id  path  string  true  "ID da regra"
// @Success      204
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /tag-rules/{id} [delete]
func (h *TagRuleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	ruleID, err := bson.ObjectIDFromHex(r.PathValue("id"))
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_VALIDATION", "ID de regra inválido")
		return
	}

	familyID, _ := bson.ObjectIDFromHex(claims.FamilyID)

	rule, err := h.ruleRepo.FindByID(r.Context(), ruleID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Regra não encontrada")
			return
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha interna")
		return
	}

	if rule.IsSystem {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Regras de sistema não podem ser deletadas")
		return
	}
	if rule.FamilyID == nil || *rule.FamilyID != familyID {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Esta regra não pertence à sua família")
		return
	}

	if err := h.ruleRepo.Delete(r.Context(), ruleID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao deletar regra")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
