package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/willGabrielPereira/finager-backend/internal/classifier"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/ofxparser"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

const maxUploadSize = 10 << 20 // 10 MB

// TransactionHandler agrupa os handlers relacionados a transações.
type TransactionHandler struct {
	txRepo    *repository.TransactionRepository
	accRepo   *repository.AccountRepository
	tagRepo   *repository.TagRepository
	stateRepo *repository.ClassifierStateRepository
}

// NewTransactionHandler cria um TransactionHandler com repositórios e lógicas injetadas.
func NewTransactionHandler(
	txRepo *repository.TransactionRepository,
	accRepo *repository.AccountRepository,
	tagRepo *repository.TagRepository,
	stateRepo *repository.ClassifierStateRepository,
) *TransactionHandler {
	return &TransactionHandler{
		txRepo:    txRepo,
		accRepo:   accRepo,
		tagRepo:   tagRepo,
		stateRepo: stateRepo,
	}
}

type importResponse struct {
	Inserted int    `json:"inserted"`
	Skipped  int    `json:"skipped"`
	Message  string `json:"message"`
}

// Import processa o upload de um arquivo OFX e persiste as transações no MongoDB.
// Cada transação é vinculada à família do usuário autenticado (family_id) e ao
// usuário que realizou o import (created_by).
//
// @Summary      Importar OFX
// @Description  Recebe um arquivo OFX via multipart/form-data e salva as transações. Duplicatas (mesmo FITID + account + family) são ignoradas.
// @Tags         transactions
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        file  formData  file            true  "Arquivo OFX"
// @Success      200   {object}  importResponse
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      422   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /transactions/import [post]
func (h *TransactionHandler) Import(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Token ausente ou malformado")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "A família vinculada a este login é inválida")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "O token atrelado a este usuário é inválido")
		return
	}

	// Limita o tamanho do body para evitar uploads gigantes.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		response.Validations(w, response.ValidationError{
			Field: "file", Rule: "max_size", Message: "Arquivo extrapola o tamanho máximo permitido ou formato rejeitado",
		})
		return
	}

	// ==== OBRIGATORIEDADE DE CONTA BANCÁRIA VINCULADA ====
	accountIDHex := r.FormValue("account_id")
	if accountIDHex == "" {
		response.Validations(w, response.ValidationError{
			Field: "account_id", Rule: "required", Message: "O campo é obrigatório para importação",
		})
		return
	}
	
	accountID, err := uuid.Parse(accountIDHex)
	if err != nil {
		response.Validations(w, response.ValidationError{
			Field: "account_id", Rule: "objectid", Message: "Identificador de formulário enviado num formato corrompido",
		})
		return
	}
	
	// Certificar autorização do usuário à conta antes do Upload
	visibleAccs, err := h.accRepo.FindVisibleAccounts(r.Context(), familyID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha DB ao processar contas visíveis")
		return
	}
	
	allowedToImport := false
	for _, acc := range visibleAccs {
		if acc.ID == accountID {
			allowedToImport = true
			break
		}
	}
	
	if !allowedToImport {
		response.Error(w, http.StatusForbidden, "E_FORBIDDEN", "Você não tem permissão para acessar a conta solicitada")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		response.Validations(w, response.ValidationError{
			Field: "file", Rule: "required", Message: "Arquivo inalcançável ou descartado pela requisição",
		})
		return
	}
	defer file.Close()

	transactions, err := ofxparser.Parse(file, accountID, familyID, userID)
	if err != nil {
		response.Validations(w, response.ValidationError{
			Field: "file", Rule: "invalid_format", Message: "O arquivo OFX está corrompido ou fora do formato estrito: " + err.Error(),
		})
		return
	}

	// Inicializa o classificador local persistido de forma otimizada O(1)
	c, err := classifier.GetOrBuildForFamily(r.Context(), &familyID, h.tagRepo, h.txRepo, h.stateRepo)
	if err != nil {
		c = nil
	}

	// Stamp every transaction with the authenticated user's family and identity, e a CONTA BANCÁRIA
	for i := range transactions {
		transactions[i].FamilyID = familyID
		transactions[i].CreatedBy = userID
		transactions[i].AccountID = accountID // O ID do mongo verdadeiro ao invés do metadado sujo do banco

		// Auto-tagging inteligente com IA Local (Naive Bayes)
		if c != nil {
			if suggested := c.Classify(transactions[i].Name + " " + transactions[i].Memo); len(suggested) > 0 {
				transactions[i].Tags = suggested
			}
		}
	}

	result, err := h.txRepo.BulkUpsert(r.Context(), transactions)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Erro interno ao salvar transações no DB")
		return
	}

	response.JSON(w, http.StatusOK, importResponse{
		Inserted: result.Inserted,
		Skipped:  result.Skipped,
		Message:  "importação concluída",
	})
}

// List retorna transações paginadas com filtros opcionais via query string.
// Sempre filtra pelo family_id do usuário autenticado.
//
// @Summary      Listar transações
// @Description  Retorna uma página de transações da família do usuário autenticado, com filtros opcionais. Ordenado por data decrescente.
// @Tags         transactions
// @Produce      json
// @Security     BearerAuth
// @Param        page        query  int     false  "Página (default: 1)"
// @Param        limit       query  int     false  "Itens por página, máx 100 (default: 20)"
// @Param        tag         query  string  false  "Filtrar por tag"
// @Param        type        query  string  false  "Tipo: DEBIT ou CREDIT"
// @Param        date_from   query  string  false  "Data inicial (YYYY-MM-DD ou RFC3339)"
// @Param        date_to     query  string  false  "Data final inclusiva (YYYY-MM-DD ou RFC3339)"
// @Param        amount_min  query  number  false  "Valor mínimo"
// @Param        amount_max  query  number  false  "Valor máximo"
// @Success      200  {object}  repository.PagedResult
// @Failure      401  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /transactions [get]
func (h *TransactionHandler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Token ausente ou inválido")
		return
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "invalid family in token")
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "invalid user in token")
		return
	}

	q := r.URL.Query()

	page := queryInt(q.Get("page"), 1)
	if page < 1 {
		page = 1
	}

	limit := queryInt(q.Get("limit"), 20)
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Identifica quais contas bancárias o usuário tem acesso antes de prosseguir
	visibleAccs, err := h.accRepo.FindVisibleAccounts(r.Context(), familyID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "falha ao checar contas visíveis")
		return
	}
	
	// Extrai apenas os IDs validos
	var allowedAccountIDs []uuid.UUID
	for _, acc := range visibleAccs {
		allowedAccountIDs = append(allowedAccountIDs, acc.ID)
	}

	// Se não veio NENHUMA conta visível, ele não pode ver NENHUMA transação (proteção hard).
	if len(allowedAccountIDs) == 0 {
		response.JSON(w, http.StatusOK, repository.PagedResult{
			Data:       []models.Transaction{},
			Total:      0,
			Page:       page,
			Limit:      limit,
			TotalPages: 0,
		})
		return
	}

	filter := repository.ListFilter{
		FamilyID:          familyID,
		AllowedAccountIDs: allowedAccountIDs,
		Page:              page,
		Limit:             limit,
		Type:              q.Get("type"),
	}

	if v := q.Get("tag"); v != "" {
		if tagID, err := uuid.Parse(v); err == nil {
			filter.TagID = &tagID
		}
	}

	if v := q.Get("date_from"); v != "" {
		if t, err := parseDate(v); err == nil {
			filter.DateFrom = t
		}
	}
	if v := q.Get("date_to"); v != "" {
		if t, err := parseDate(v); err == nil {
			// Inclui o dia inteiro: vai até 23:59:59 do dia informado.
			filter.DateTo = t.Add(24*time.Hour - time.Second)
		}
	}

	if v := q.Get("amount_min"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			filter.AmountMin = &f
		}
	}
	if v := q.Get("amount_max"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			filter.AmountMax = &f
		}
	}

	result, err := h.txRepo.List(r.Context(), filter)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "falha ao buscar transações")
		return
	}

	response.JSON(w, http.StatusOK, result)
}

// queryInt converte uma string para int, retornando fallback em caso de falha.
func queryInt(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

// parseDate tenta parsear uma data nos formatos RFC3339 e YYYY-MM-DD.
func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}

// updateTransactionRequest DTO para alteração de tags
type updateTransactionRequest struct {
	Tags []string `json:"tags"`
}

// Update altera as tags de uma transação garantindo segurança multi-tenant
func (h *TransactionHandler) Update(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	tx, _, ok := ResolveTransactionForFamily(w, r, h.txRepo)
	if !ok {
		return
	}

	var req updateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload inválido"})
		return
	}

	// Conversão de IDs de tag para bson.ObjectID
	var tagIDs []uuid.UUID
	for _, tagHex := range req.Tags {
		tagID, err := uuid.Parse(tagHex)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "E_VALIDATION", "ID de tag inválido: "+tagHex)
			return
		}
		tagIDs = append(tagIDs, tagID)
	}

	// Atualizar tags da transação no banco
	if err := h.txRepo.UpdateTags(r.Context(), tx.ID, tagIDs); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao salvar as tags da transação")
		return
	}

	// Reconstrói o estado do classificador em background de forma assíncrona
	go func(fID *uuid.UUID) {
		_, _ = classifier.RebuildStateForFamily(context.Background(), fID, h.tagRepo, h.txRepo, h.stateRepo)
	}(&tx.FamilyID)

	w.WriteHeader(http.StatusNoContent)
}

// ResolveTransactionForFamily valida o ID da rota, valida a sessão (claims),
// busca a transação no banco e garante o isolamento multi-tenant (RLS) da família.
// Retorna a transação e o ObjectID da família em caso de sucesso.
// Retorna a transação e o UUID da família em caso de sucesso.
// Se houver qualquer falha, escreve a resposta de erro apropriada no ResponseWriter
// e retorna (nil, uuid.Nil, false).
func ResolveTransactionForFamily(
	w http.ResponseWriter,
	r *http.Request,
	txRepo *repository.TransactionRepository,
) (*models.Transaction, uuid.UUID, bool) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.Error(w, http.StatusUnauthorized, "E_UNAUTHORIZED", "Sessão expirada ou não fornecida")
		return nil, uuid.Nil, false
	}

	txIDHex := r.PathValue("id")
	txID, err := uuid.Parse(txIDHex)
	if err != nil {
		response.Error(w, http.StatusBadRequest, "E_VALIDATION", "O ID da transação informado é inválido")
		return nil, uuid.Nil, false
	}

	familyID, err := uuid.Parse(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "Família inválida no token")
		return nil, uuid.Nil, false
	}

	// Busca a transação e garante o isolamento multi-tenant combinando ID e FamilyID na consulta
	tx, err := txRepo.FindByIDAndFamily(r.Context(), txID, familyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "E_NOT_FOUND", "Transação não localizada")
			return nil, uuid.Nil, false
		}
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha interna ao buscar transação")
		return nil, uuid.Nil, false
	}

	return tx, familyID, true
}

