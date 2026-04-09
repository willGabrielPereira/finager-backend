package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

const collectionName = "transactions"

// TransactionRepository encapsula as operações de persistência de Transaction.
type TransactionRepository struct {
	col *mongo.Collection
}

// NewTransactionRepository cria um repositório apontando para a collection correta.
func NewTransactionRepository(db *mongo.Database) *TransactionRepository {
	return &TransactionRepository{col: db.Collection(collectionName)}
}

// UpsertResult resume o resultado de uma operação de upsert em lote.
type UpsertResult struct {
	Inserted int
	Skipped  int
}

// BulkUpsert insere transações novas e ignora as que já existem, usando
// o par (fitid, account_id) como chave de unicidade.
// Retorna um resumo de quantas foram inseridas e quantas já existiam.
func (r *TransactionRepository) BulkUpsert(ctx context.Context, txs []models.Transaction) (UpsertResult, error) {
	if len(txs) == 0 {
		return UpsertResult{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var result UpsertResult

	for _, tx := range txs {
		filter := bson.D{
			{Key: "fitid", Value: tx.FITID},
			{Key: "account_id", Value: tx.AccountID},
		}

		update := bson.D{
			{Key: "$setOnInsert", Value: tx},
		}

		opts := options.UpdateOne().SetUpsert(true)
		res, err := r.col.UpdateOne(ctx, filter, update, opts)
		if err != nil {
			return result, err
		}

		if res.UpsertedCount > 0 {
			result.Inserted++
		} else {
			result.Skipped++
		}
	}

	return result, nil
}

// ListFilter agrupa todos os filtros aceitos pela operação List.
type ListFilter struct {
	Tag       string
	Type      string // DEBIT | CREDIT
	DateFrom  time.Time
	DateTo    time.Time
	AmountMin *float64
	AmountMax *float64
	Page      int
	Limit     int
}

// PagedResult é o envelope de resposta paginada para transações.
type PagedResult struct {
	Data       []models.Transaction `json:"data"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	Limit      int                  `json:"limit"`
	TotalPages int                  `json:"total_pages"`
}

// List retorna uma página de transações aplicando os filtros fornecidos.
// Os resultados são ordenados por date_posted decrescente (mais recente primeiro).
func (r *TransactionRepository) List(ctx context.Context, f ListFilter) (PagedResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	filter := bson.D{}

	if f.Tag != "" {
		filter = append(filter, bson.E{Key: "tags", Value: f.Tag})
	}

	if f.Type != "" {
		filter = append(filter, bson.E{Key: "type", Value: f.Type})
	}

	if !f.DateFrom.IsZero() || !f.DateTo.IsZero() {
		dateFilter := bson.D{}
		if !f.DateFrom.IsZero() {
			dateFilter = append(dateFilter, bson.E{Key: "$gte", Value: f.DateFrom})
		}
		if !f.DateTo.IsZero() {
			dateFilter = append(dateFilter, bson.E{Key: "$lte", Value: f.DateTo})
		}
		filter = append(filter, bson.E{Key: "date_posted", Value: dateFilter})
	}

	if f.AmountMin != nil || f.AmountMax != nil {
		amountFilter := bson.D{}
		if f.AmountMin != nil {
			amountFilter = append(amountFilter, bson.E{Key: "$gte", Value: *f.AmountMin})
		}
		if f.AmountMax != nil {
			amountFilter = append(amountFilter, bson.E{Key: "$lte", Value: *f.AmountMax})
		}
		filter = append(filter, bson.E{Key: "amount", Value: amountFilter})
	}

	total, err := r.col.CountDocuments(ctx, filter)
	if err != nil {
		return PagedResult{}, err
	}

	skip := int64((f.Page - 1) * f.Limit)
	opts := options.Find().
		SetSort(bson.D{{Key: "date_posted", Value: -1}}).
		SetSkip(skip).
		SetLimit(int64(f.Limit))

	cursor, err := r.col.Find(ctx, filter, opts)
	if err != nil {
		return PagedResult{}, err
	}
	defer cursor.Close(ctx)

	var txs []models.Transaction
	if err := cursor.All(ctx, &txs); err != nil {
		return PagedResult{}, err
	}

	totalPages := int((total + int64(f.Limit) - 1) / int64(f.Limit))

	return PagedResult{
		Data:       txs,
		Total:      total,
		Page:       f.Page,
		Limit:      f.Limit,
		TotalPages: totalPages,
	}, nil
}

