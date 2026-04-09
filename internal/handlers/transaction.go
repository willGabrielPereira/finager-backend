package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/willGabrielPereira/finager-backend/internal/ofxparser"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

const maxUploadSize = 10 << 20 // 10 MB

// TransactionHandler agrupa os handlers relacionados a transações.
type TransactionHandler struct {
	repo *repository.TransactionRepository
}

// NewTransactionHandler cria um TransactionHandler com o repositório injetado.
func NewTransactionHandler(repo *repository.TransactionRepository) *TransactionHandler {
	return &TransactionHandler{repo: repo}
}

type importResponse struct {
	Inserted int    `json:"inserted"`
	Skipped  int    `json:"skipped"`
	Message  string `json:"message"`
}

// Import processa o upload de um arquivo OFX e persiste as transações no MongoDB.
//
// @Summary      Importar OFX
// @Description  Recebe um arquivo OFX via multipart/form-data e salva as transações. Duplicatas (mesmo FITID + account) são ignoradas.
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

	// Limita o tamanho do body para evitar uploads gigantes.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "arquivo muito grande ou formato inválido"})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "campo 'file' não encontrado no formulário"})
		return
	}
	defer file.Close()

	transactions, err := ofxparser.Parse(file)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "falha ao processar o arquivo OFX: " + err.Error()})
		return
	}

	result, err := h.repo.BulkUpsert(r.Context(), transactions)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "falha ao salvar as transações"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(importResponse{
		Inserted: result.Inserted,
		Skipped:  result.Skipped,
		Message:  "importação concluída",
	})
}

// List retorna transações paginadas com filtros opcionais via query string.
//
// @Summary      Listar transações
// @Description  Retorna uma página de transações com filtros opcionais. Ordenado por data decrescente.
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

	filter := repository.ListFilter{
		Page:  page,
		Limit: limit,
		Tag:   q.Get("tag"),
		Type:  q.Get("type"),
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

	result, err := h.repo.List(r.Context(), filter)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "falha ao buscar transações"})
		return
	}

	_ = json.NewEncoder(w).Encode(result)
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
