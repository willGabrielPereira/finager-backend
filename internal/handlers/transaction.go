package handlers

import (
	"net/http"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/ofxparser"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/response"
)

const maxUploadSize = 10 << 20 // 10 MB

// TransactionHandler agrupa os handlers relacionados a transações.
type TransactionHandler struct {
	txRepo  *repository.TransactionRepository
	accRepo *repository.AccountRepository
}

// NewTransactionHandler cria um TransactionHandler com repositórios e lógicas injetadas.
func NewTransactionHandler(txRepo *repository.TransactionRepository, accRepo *repository.AccountRepository) *TransactionHandler {
	return &TransactionHandler{txRepo: txRepo, accRepo: accRepo}
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

	familyID, err := bson.ObjectIDFromHex(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "A família vinculada a este login é inválida")
		return
	}

	userID, err := bson.ObjectIDFromHex(claims.UserID)
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
	
	accountID, err := bson.ObjectIDFromHex(accountIDHex)
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

	transactions, err := ofxparser.Parse(file)
	if err != nil {
		response.Validations(w, response.ValidationError{
			Field: "file", Rule: "invalid_format", Message: "O arquivo OFX está corrompido ou fora do formato estrito: " + err.Error(),
		})
		return
	}

	// Stamp every transaction with the authenticated user's family and identity, e a CONTA BANCÁRIA
	for i := range transactions {
		transactions[i].FamilyID = familyID
		transactions[i].CreatedBy = userID
		transactions[i].AccountID = accountID // O ID do mongo verdadeiro ao invés do metadado sujo do banco
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

	familyID, err := bson.ObjectIDFromHex(claims.FamilyID)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "E_INVALID_SESSION", "invalid family in token")
		return
	}

	userID, err := bson.ObjectIDFromHex(claims.UserID)
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
	var allowedAccountIDs []bson.ObjectID
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
		AllowedAccountIDs: allowedAccountIDs, // Injetando o funil de restrição
		Page:              page,
		Limit:             limit,
		Tag:               q.Get("tag"),
		Type:              q.Get("type"),
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
