package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
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
	txRepo       *repository.TransactionRepository
	accRepo      *repository.AccountRepository
	tagRepo      *repository.TagRepository
	stateRepo    *repository.ClassifierStateRepository
	merchantRepo *repository.MerchantMappingRepository
}

// NewTransactionHandler cria um TransactionHandler com repositórios e lógicas injetadas.
func NewTransactionHandler(
	txRepo *repository.TransactionRepository,
	accRepo *repository.AccountRepository,
	tagRepo *repository.TagRepository,
	stateRepo *repository.ClassifierStateRepository,
	merchantRepo *repository.MerchantMappingRepository,
) *TransactionHandler {
	return &TransactionHandler{
		txRepo:       txRepo,
		accRepo:      accRepo,
		tagRepo:      tagRepo,
		stateRepo:    stateRepo,
		merchantRepo: merchantRepo,
	}
}

type importResponse struct {
	Inserted   int    `json:"inserted"`
	Skipped    int    `json:"skipped"`
	Reconciled int    `json:"reconciled"`
	Message    string `json:"message"`
}

// Import processa o upload de um arquivo OFX (banco ou cartão) e persiste as transações no banco.
// Suporta conciliação automática com pagamentos manuais ou planejados prévios e classificação híbrida.
// @Summary      Importar OFX
// @Description  Recebe um arquivo OFX via multipart/form-data e salva as transações com conciliação automática.
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

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		response.Validations(w, response.ValidationError{
			Field: "file", Rule: "max_size", Message: "Arquivo extrapola o tamanho máximo permitido ou formato rejeitado",
		})
		return
	}

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
			Field: "account_id", Rule: "uuid", Message: "Identificador de formulário enviado num formato corrompido",
		})
		return
	}

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

	var toInsert []models.Transaction
	reconciledCount := 0

	for i := range transactions {
		transactions[i].FamilyID = familyID
		transactions[i].CreatedBy = userID
		transactions[i].AccountID = accountID

		// 1. Conciliação Automática: busca se já existe lançamento manual ou planejado equivalente
		match, err := h.txRepo.FindReconciliationMatch(r.Context(), familyID, accountID, transactions[i].Amount, transactions[i].DatePosted)
		if err == nil && match != nil {
			_ = h.txRepo.Reconcile(r.Context(), match.ID, familyID, transactions[i].FITID, transactions[i].DatePosted)
			reconciledCount++
			continue
		}

		// 2. Classificação em Camadas:
		// Camada 1: Memória de Estabelecimentos (100% de precisão)
		if matchTagID, err := h.merchantRepo.FindMatch(r.Context(), familyID, transactions[i].Name+" "+transactions[i].Memo); err == nil && matchTagID != nil {
			transactions[i].Tags = []uuid.UUID{*matchTagID}
		} else if c != nil {
			// Camada 2: Naive Bayes com limiar de confiança
			if suggested := c.Classify(transactions[i].Name + " " + transactions[i].Memo); len(suggested) > 0 {
				transactions[i].Tags = suggested
			}
		}

		toInsert = append(toInsert, transactions[i])
	}

	result, err := h.txRepo.BulkUpsert(r.Context(), toInsert)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Erro interno ao salvar transações no DB")
		return
	}

	response.JSON(w, http.StatusOK, importResponse{
		Inserted:   result.Inserted,
		Skipped:    result.Skipped,
		Reconciled: reconciledCount,
		Message:    "importação concluída",
	})
}

// createTransactionRequest DTO para criação manual ou planejada de transação
type createTransactionRequest struct {
	AccountID            string   `json:"account_id"`
	Type                 string   `json:"type"` // DEBIT, CREDIT
	DatePosted           string   `json:"date_posted"`
	Amount               float64  `json:"amount"`
	Name                 string   `json:"name"`
	Memo                 string   `json:"memo"`
	Tags                 []string `json:"tags"`
	Status               string   `json:"status"` // POSTED, PLANNED, PENDING_RECONCILIATION
	IsTransfer           bool     `json:"is_transfer"`
	DestinationAccountID string   `json:"destination_account_id"`
}

// Create cria manualmente uma transação (avulsa, planejada ou pendente de conciliação)
// @Summary      Criar transação manual
// @Description  Registra uma despesa ou receita manualmente, ou agenda um pagamento previsto.
// @Router       /transactions [post]
func (h *TransactionHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req createTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload inválido"})
		return
	}

	accountID, err := uuid.Parse(req.AccountID)
	if err != nil {
		response.Validations(w, response.ValidationError{Field: "account_id", Rule: "required", Message: "Conta bancária inválida ou ausente"})
		return
	}

	datePosted := time.Now()
	if req.DatePosted != "" {
		if t, err := parseDate(req.DatePosted); err == nil {
			datePosted = t
		}
	}

	status := req.Status
	if status == "" {
		status = models.TxStatusPosted
	}

	var tagIDs []uuid.UUID
	for _, tagHex := range req.Tags {
		if tID, err := uuid.Parse(tagHex); err == nil {
			tagIDs = append(tagIDs, tID)
		}
	}

	var destAccID *uuid.UUID
	if req.DestinationAccountID != "" {
		if dID, err := uuid.Parse(req.DestinationAccountID); err == nil {
			destAccID = &dID
		}
	}

	tx := &models.Transaction{
		AccountID:            accountID,
		FamilyID:             familyID,
		CreatedBy:            userID,
		Type:                 req.Type,
		DatePosted:           datePosted,
		Amount:               req.Amount,
		Name:                 req.Name,
		Memo:                 req.Memo,
		Tags:                 tagIDs,
		ManuallyTagged:       len(tagIDs) > 0,
		Status:               status,
		IsTransfer:           req.IsTransfer,
		DestinationAccountID: destAccID,
		Source:               models.TxSourceManual,
	}

	created, err := h.txRepo.Create(r.Context(), tx)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao criar transação no DB: "+err.Error())
		return
	}

	// Se houver tags atribuídas, grava na memória de comerciantes para aprendizado imediato
	if len(tagIDs) > 0 {
		cleanMerchant := classifier.CleanMerchantName(req.Name, req.Memo)
		if cleanMerchant != "" {
			_ = h.merchantRepo.Upsert(r.Context(), familyID, cleanMerchant, tagIDs[0])
		}
		go func(fID *uuid.UUID) {
			_, _ = classifier.RebuildStateForFamily(context.Background(), fID, h.tagRepo, h.txRepo, h.stateRepo)
		}(&familyID)
	}

	response.JSON(w, http.StatusCreated, created)
}

// Delete remove uma transação com isolamento multi-tenant
// @Summary      Deletar transação
// @Router       /transactions/{id} [delete]
func (h *TransactionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	tx, familyID, ok := ResolveTransactionForFamily(w, r, h.txRepo)
	if !ok {
		return
	}

	if err := h.txRepo.Delete(r.Context(), tx.ID, familyID); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao remover transação")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type applySimilarRequest struct {
	IncludeManuallyTagged bool `json:"include_manually_tagged"`
}

// ApplySimilar propaga a tag da transação atual para todas as transações com nome/memo similar
// @Summary      Propagar tag para transações similares
// @Router       /transactions/{id}/apply-similar [post]
func (h *TransactionHandler) ApplySimilar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	tx, familyID, ok := ResolveTransactionForFamily(w, r, h.txRepo)
	if !ok {
		return
	}

	if len(tx.Tags) == 0 {
		response.Error(w, http.StatusBadRequest, "E_VALIDATION", "A transação selecionada não possui nenhuma tag atribuída")
		return
	}

	var req applySimilarRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	cleanPattern := classifier.CleanMerchantName(tx.Name, tx.Memo)
	if cleanPattern == "" {
		cleanPattern = tx.Name
	}

	count, err := h.txRepo.ApplyTagToSimilar(r.Context(), familyID, cleanPattern, tx.Tags[0], req.IncludeManuallyTagged)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao propagar tags")
		return
	}

	// Atualiza memória de comerciantes
	_ = h.merchantRepo.Upsert(r.Context(), familyID, cleanPattern, tx.Tags[0])

	// Reconstrói modelo de IA
	go func(fID *uuid.UUID) {
		_, _ = classifier.RebuildStateForFamily(context.Background(), fID, h.tagRepo, h.txRepo, h.stateRepo)
	}(&familyID)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"updated_count": count,
		"pattern":       cleanPattern,
		"tag_id":        tx.Tags[0],
	})
}

// List retorna transações paginadas com filtros opcionais via query string.
// @Summary      Listar transações
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

	visibleAccs, err := h.accRepo.FindVisibleAccounts(r.Context(), familyID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "falha ao checar contas visíveis")
		return
	}

	var allowedAccountIDs []uuid.UUID
	for _, acc := range visibleAccs {
		allowedAccountIDs = append(allowedAccountIDs, acc.ID)
	}

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
		Status:            q.Get("status"),
		Search:            q.Get("search"),
	}

	// Parsing de filtro multi-select de contas
	var filterAccountIDs []uuid.UUID
	for _, accParam := range append(q["account_id"], q["accounts"]...) {
		for _, part := range strings.Split(accParam, ",") {
			if aID, err := uuid.Parse(strings.TrimSpace(part)); err == nil {
				filterAccountIDs = append(filterAccountIDs, aID)
			}
		}
	}
	filter.AccountIDs = filterAccountIDs

	// Parsing de filtro multi-select de tags/categorias
	var filterTagIDs []uuid.UUID
	for _, tagParam := range append(q["tag"], q["tags"]...) {
		for _, part := range strings.Split(tagParam, ",") {
			if tID, err := uuid.Parse(strings.TrimSpace(part)); err == nil {
				filterTagIDs = append(filterTagIDs, tID)
			}
		}
	}
	filter.TagIDs = filterTagIDs
	if len(filterTagIDs) == 1 {
		filter.TagID = &filterTagIDs[0]
	}

	if v := q.Get("date_from"); v != "" {
		if t, err := parseDate(v); err == nil {
			filter.DateFrom = t
		}
	}
	if v := q.Get("date_to"); v != "" {
		if t, err := parseDate(v); err == nil {
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

func parseDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}

type updateTransactionRequest struct {
	Tags []string `json:"tags"`
}

// Update altera as tags de uma transação e alimenta a memória de comerciantes
func (h *TransactionHandler) Update(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	tx, familyID, ok := ResolveTransactionForFamily(w, r, h.txRepo)
	if !ok {
		return
	}

	var req updateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Validations(w, response.ValidationError{Field: "payload", Rule: "malformed", Message: "Payload inválido"})
		return
	}

	var tagIDs []uuid.UUID
	for _, tagHex := range req.Tags {
		tagID, err := uuid.Parse(tagHex)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "E_VALIDATION", "ID de tag inválido: "+tagHex)
			return
		}
		tagIDs = append(tagIDs, tagID)
	}

	if err := h.txRepo.UpdateTags(r.Context(), tx.ID, tagIDs); err != nil {
		response.Error(w, http.StatusInternalServerError, "E_INTERNAL", "Falha ao salvar as tags da transação")
		return
	}

	// Alimenta memória de comerciantes para match instantâneo em próximas importações
	if len(tagIDs) > 0 {
		cleanPattern := classifier.CleanMerchantName(tx.Name, tx.Memo)
		if cleanPattern != "" {
			_ = h.merchantRepo.Upsert(r.Context(), familyID, cleanPattern, tagIDs[0])
		}
	}

	go func(fID *uuid.UUID) {
		_, _ = classifier.RebuildStateForFamily(context.Background(), fID, h.tagRepo, h.txRepo, h.stateRepo)
	}(&tx.FamilyID)

	w.WriteHeader(http.StatusNoContent)
}

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
