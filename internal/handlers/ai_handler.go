package handlers

import (
	"context"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/classifier"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

// AIHandler agrupa os handlers para classificação de transações usando inteligência artificial local.
type AIHandler struct {
	txRepo    *repository.TransactionRepository
	tagRepo   *repository.TagRepository
	stateRepo *repository.ClassifierStateRepository
}

// NewAIHandler cria uma nova instância de AIHandler.
func NewAIHandler(
	txRepo *repository.TransactionRepository,
	tagRepo *repository.TagRepository,
	stateRepo *repository.ClassifierStateRepository,
) *AIHandler {
	return &AIHandler{
		txRepo:    txRepo,
		tagRepo:   tagRepo,
		stateRepo: stateRepo,
	}
}

// SuggestTags sugere tags para uma transação específica usando o classificador local Naive Bayes.
// Não persiste as tags no banco, apenas retorna a sugestão de tag calculada estatisticamente.
//
// @Summary      Sugerir Tags via IA
// @Description  Treina o classificador Bayesiano local com dados estáticos + histórico e sugere tags para a transação.
// @Tags         ai
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "ID da Transação"
// @Success      200  {array}   models.Tag
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /transactions/{id}/suggest-tags [post]
func (h *AIHandler) SuggestTags(w http.ResponseWriter, r *http.Request) {
	tx, familyID, ok := ResolveTransactionForFamily(w, r, h.txRepo)
	if !ok {
		return
	}

	// 2. Carrega o classificador persistido de forma otimizada O(1)
	c, err := classifier.GetOrBuildForFamily(r.Context(), familyID, h.tagRepo, h.txRepo, h.stateRepo)
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

// AutoTagBatch treina o classificador local e atualiza em lote todas as transações sem tags da família.
//
// @Summary      Auto-taggear Transações em Lote
// @Description  Busca todas as transações sem tags da família, treina o classificador local e aplica as tags sugeridas no MongoDB.
// @Tags         ai
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]interface{}
// @Failure      401  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /transactions/ai-auto-tag [post]
func (h *AIHandler) AutoTagBatch(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Não autenticado")
		return
	}

	familyID, err := bson.ObjectIDFromHex(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Sessão inválida")
		return
	}

	// 1. Busca transações sem tags
	untaggedTxs, err := h.txRepo.FindAllUntagged(r.Context(), familyID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao buscar transações sem tags")
		return
	}

	if len(untaggedTxs) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"tagged_count": 0,
			"message":      "Nenhuma transação sem tags encontrada para processar.",
		})
		return
	}

	// 2. Carrega o classificador persistido de forma otimizada O(1)
	c, err := classifier.GetOrBuildForFamily(r.Context(), familyID, h.tagRepo, h.txRepo, h.stateRepo)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao inicializar o classificador de IA")
		return
	}

	// 3. Classifica e atualiza as transações no banco de dados
	taggedCount := 0
	for _, tx := range untaggedTxs {
		suggestedTagIDs := c.Classify(tx.Name + " " + tx.Memo)
		if len(suggestedTagIDs) > 0 {
			err := h.txRepo.UpdateTags(r.Context(), tx.ID, suggestedTagIDs)
			if err == nil {
				taggedCount++
			}
		}
	}

	// 4. Reconstrói o estado compilado da IA em background após as atualizações
	go func(fID bson.ObjectID) {
		_, _ = classifier.RebuildStateForFamily(context.Background(), fID, h.tagRepo, h.txRepo, h.stateRepo)
	}(familyID)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"tagged_count": taggedCount,
		"message":      "Auto-tagging em lote concluído com sucesso.",
	})
}
