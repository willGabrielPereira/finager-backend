package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/willGabrielPereira/finager-backend/internal/classifier"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

// AIHandler agrupa os handlers para classificação de transações usando memória e inteligência artificial.
type AIHandler struct {
	txRepo       *repository.TransactionRepository
	tagRepo      *repository.TagRepository
	stateRepo    *repository.ClassifierStateRepository
	merchantRepo *repository.MerchantMappingRepository
}

// NewAIHandler cria uma nova instância de AIHandler.
func NewAIHandler(
	txRepo *repository.TransactionRepository,
	tagRepo *repository.TagRepository,
	stateRepo *repository.ClassifierStateRepository,
	merchantRepo *repository.MerchantMappingRepository,
) *AIHandler {
	return &AIHandler{
		txRepo:       txRepo,
		tagRepo:      tagRepo,
		stateRepo:    stateRepo,
		merchantRepo: merchantRepo,
	}
}

// SuggestTags sugere tags para uma transação específica usando primeiro a memória de estabelecimentos e depois Naive Bayes.
// @Summary      Sugerir Tags via IA
// @Description  Verifica memória de comerciantes da família e classificador Bayesiano local.
// @Router       /transactions/{id}/suggest-tags [post]
func (h *AIHandler) SuggestTags(w http.ResponseWriter, r *http.Request) {
	tx, familyID, ok := ResolveTransactionForFamily(w, r, h.txRepo)
	if !ok {
		return
	}

	// 1. Camada 1: Memória de Estabelecimentos (100% de confiança)
	if matchTagID, err := h.merchantRepo.FindMatch(r.Context(), familyID, tx.Name+" "+tx.Memo); err == nil && matchTagID != nil {
		tag, err := h.tagRepo.FindByID(r.Context(), *matchTagID)
		if err == nil && tag != nil {
			response.JSON(w, http.StatusOK, []*models.Tag{tag})
			return
		}
	}

	// 2. Camada 2: Carrega o classificador persistido de forma otimizada O(1)
	c, err := classifier.GetOrBuildForFamily(r.Context(), &familyID, h.tagRepo, h.txRepo, h.stateRepo)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao inicializar o classificador de IA")
		return
	}

	// 3. Classifica a transação
	suggestedTagIDs := c.Classify(tx.Name + " " + tx.Memo)

	// 4. Resolve IDs de tags sugeridos para objetos Tag completos
	var suggestedTags []*models.Tag
	for _, tagID := range suggestedTagIDs {
		tag, err := h.tagRepo.FindByID(r.Context(), tagID)
		if err == nil && tag != nil {
			suggestedTags = append(suggestedTags, tag)
		}
	}

	if suggestedTags == nil {
		suggestedTags = []*models.Tag{}
	}

	response.JSON(w, http.StatusOK, suggestedTags)
}

type autoTagBatchRequest struct {
	IncludeManuallyTagged bool `json:"include_manually_tagged"`
}

// AutoTagBatch executa classificação em lote combinando memória de estabelecimentos e classificador estatístico.
// @Summary      Auto-taggear Transações em Lote
// @Description  Aplica tags sugeridas por memória e IA, com opção de incluir ou não transações já ajustadas manualmente.
// @Router       /transactions/ai-auto-tag [post]
func (h *AIHandler) AutoTagBatch(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	var req autoTagBatchRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// 1. Busca transações para classificar
	targetTxs, err := h.txRepo.FindAllUntagged(r.Context(), familyID, req.IncludeManuallyTagged)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao buscar transações")
		return
	}

	if len(targetTxs) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"tagged_count": 0,
			"message":      "Nenhuma transação elegível encontrada para processar.",
		})
		return
	}

	// 2. Carrega o classificador persistido O(1)
	c, err := classifier.GetOrBuildForFamily(r.Context(), &familyID, h.tagRepo, h.txRepo, h.stateRepo)
	if err != nil {
		c = nil
	}

	// 3. Classifica e atualiza as transações no banco de dados
	taggedCount := 0
	for _, tx := range targetTxs {
		var tagIDs []uuid.UUID

		// Camada 1: Memória de comerciantes
		if matchTagID, err := h.merchantRepo.FindMatch(r.Context(), familyID, tx.Name+" "+tx.Memo); err == nil && matchTagID != nil {
			tagIDs = []uuid.UUID{*matchTagID}
		} else if c != nil {
			// Camada 2: Naive Bayes com limiar de confiança
			if suggested := c.Classify(tx.Name + " " + tx.Memo); len(suggested) > 0 {
				tagIDs = suggested
			}
		}

		if len(tagIDs) > 0 {
			err := h.txRepo.UpdateTags(r.Context(), tx.ID, tagIDs)
			if err == nil {
				taggedCount++
			}
		}
	}

	// 4. Reconstrói o estado compilado da IA em background após as atualizações
	go func(fID *uuid.UUID) {
		_, _ = classifier.RebuildStateForFamily(context.Background(), fID, h.tagRepo, h.txRepo, h.stateRepo)
	}(&familyID)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"tagged_count": taggedCount,
		"message":      "Classificação em lote concluída com sucesso.",
	})
}

// ListRules lista todas as regras de estabelecimentos (Layer 1) da família.
// @Summary      Listar Regras de Estabelecimento
// @Router       /merchant-rules [get]
func (h *AIHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	rules, err := h.merchantRepo.ListByFamily(r.Context(), familyID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao listar regras")
		return
	}

	if rules == nil {
		rules = []models.MerchantMapping{}
	}

	response.JSON(w, http.StatusOK, rules)
}

type createRuleRequest struct {
	Pattern string    `json:"pattern"`
	TagID   uuid.UUID `json:"tag_id"`
}

// CreateRule cria ou atualiza uma regra determinística de estabelecimento.
// @Summary      Criar/Atualizar Regra de Estabelecimento
// @Router       /merchant-rules [post]
func (h *AIHandler) CreateRule(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	var req createRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_PAYLOAD", "Payload inválido")
		return
	}

	if req.Pattern == "" || req.TagID == uuid.Nil {
		response.Error(w, http.StatusUnprocessableEntity, "E_VALIDATION", "Padrão e Tag são obrigatórios")
		return
	}

	if err := h.merchantRepo.Upsert(r.Context(), familyID, req.Pattern, req.TagID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao salvar regra")
		return
	}

	response.JSON(w, http.StatusCreated, map[string]string{"message": "Regra salva com sucesso"})
}

// DeleteRule remove uma regra de mapeamento de estabelecimento.
// @Summary      Excluir Regra de Estabelecimento
// @Router       /merchant-rules/{id} [delete]
func (h *AIHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_INVALID_ID", "ID de regra inválido")
		return
	}

	if err := h.merchantRepo.Delete(r.Context(), familyID, id); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao remover regra")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

