package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

type TransactionRepository struct {
	pool *pgxpool.Pool
}

func NewTransactionRepository(pool *pgxpool.Pool) *TransactionRepository {
	return &TransactionRepository{pool: pool}
}

type UpsertResult struct {
	Inserted int
	Skipped  int
}

func (r *TransactionRepository) BulkUpsert(ctx context.Context, txs []models.Transaction) (UpsertResult, error) {
	if len(txs) == 0 {
		return UpsertResult{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var result UpsertResult

	for _, tx := range txs {
		var id uuid.UUID
		query := `
			INSERT INTO transactions (fitid, type, date_posted, amount, name, memo, account_id, family_id, created_by, imported_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (fitid, account_id, family_id) DO NOTHING
			RETURNING id
		`
		err := r.pool.QueryRow(ctx, query, tx.FITID, tx.Type, tx.DatePosted, tx.Amount, tx.Name, tx.Memo, tx.AccountID, tx.FamilyID, tx.CreatedBy, time.Now()).Scan(&id)
		
		if err == pgx.ErrNoRows {
			result.Skipped++
			continue
		} else if err != nil {
			return result, err
		}

		result.Inserted++
		
		for _, tagID := range tx.Tags {
			_, err = r.pool.Exec(ctx, `INSERT INTO transaction_tags (transaction_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, tagID)
			if err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

type ListFilter struct {
	FamilyID          uuid.UUID
	AllowedAccountIDs []uuid.UUID
	TagID             *uuid.UUID
	Type              string
	DateFrom          time.Time
	DateTo            time.Time
	AmountMin         *float64
	AmountMax         *float64
	Page              int
	Limit             int
}

type PagedResult struct {
	Data       []models.Transaction `json:"data"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	Limit      int                  `json:"limit"`
	TotalPages int                  `json:"total_pages"`
}

func (r *TransactionRepository) List(ctx context.Context, f ListFilter) (PagedResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	whereClauses := []string{"t.family_id = $1"}
	args := []interface{}{f.FamilyID}
	argIdx := 2

	if len(f.AllowedAccountIDs) > 0 {
		var placeholders []string
		for _, id := range f.AllowedAccountIDs {
			placeholders = append(placeholders, fmt.Sprintf("$%d", argIdx))
			args = append(args, id)
			argIdx++
		}
		whereClauses = append(whereClauses, fmt.Sprintf("t.account_id IN (%s)", strings.Join(placeholders, ", ")))
	} else {
		// Se não tem conta permitida, retorna vazio
		whereClauses = append(whereClauses, "1=0") 
	}

	if f.TagID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("EXISTS (SELECT 1 FROM transaction_tags tt WHERE tt.transaction_id = t.id AND tt.tag_id = $%d)", argIdx))
		args = append(args, *f.TagID)
		argIdx++
	}

	if f.Type != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("t.type = $%d", argIdx))
		args = append(args, f.Type)
		argIdx++
	}

	if !f.DateFrom.IsZero() {
		whereClauses = append(whereClauses, fmt.Sprintf("t.date_posted >= $%d", argIdx))
		args = append(args, f.DateFrom)
		argIdx++
	}

	if !f.DateTo.IsZero() {
		whereClauses = append(whereClauses, fmt.Sprintf("t.date_posted <= $%d", argIdx))
		args = append(args, f.DateTo)
		argIdx++
	}

	if f.AmountMin != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("t.amount >= $%d", argIdx))
		args = append(args, *f.AmountMin)
		argIdx++
	}

	if f.AmountMax != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("t.amount <= $%d", argIdx))
		args = append(args, *f.AmountMax)
		argIdx++
	}

	whereStr := "WHERE " + strings.Join(whereClauses, " AND ")

	countQuery := `SELECT COUNT(*) FROM transactions t ` + whereStr
	var total int64
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return PagedResult{}, err
	}

	skip := (f.Page - 1) * f.Limit
	args = append(args, f.Limit, skip)
	query := `
		SELECT t.id, t.fitid, t.type, t.date_posted, t.amount, t.name, t.memo, t.account_id, t.family_id, t.created_by, t.imported_at,
		       COALESCE(array_agg(tt.tag_id) FILTER (WHERE tt.tag_id IS NOT NULL), '{}')
		FROM transactions t
		LEFT JOIN transaction_tags tt ON t.id = tt.transaction_id
		` + whereStr + `
		GROUP BY t.id
		ORDER BY t.date_posted DESC
		LIMIT $` + fmt.Sprintf("%d", argIdx) + ` OFFSET $` + fmt.Sprintf("%d", argIdx+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return PagedResult{}, err
	}
	defer rows.Close()

	var txs []models.Transaction
	for rows.Next() {
		var tx models.Transaction
		if err := rows.Scan(&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo, &tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt, &tx.Tags); err != nil {
			return PagedResult{}, err
		}
		if tx.Tags == nil {
			tx.Tags = []uuid.UUID{}
		}
		txs = append(txs, tx)
	}

	if txs == nil {
		txs = []models.Transaction{}
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

func (r *TransactionRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	query := `
		SELECT t.id, t.fitid, t.type, t.date_posted, t.amount, t.name, t.memo, t.account_id, t.family_id, t.created_by, t.imported_at,
		       COALESCE(array_agg(tt.tag_id) FILTER (WHERE tt.tag_id IS NOT NULL), '{}')
		FROM transactions t
		LEFT JOIN transaction_tags tt ON t.id = tt.transaction_id
		WHERE t.id = $1
		GROUP BY t.id
	`
	var tx models.Transaction
	err := r.pool.QueryRow(ctx, query, id).Scan(&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo, &tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt, &tx.Tags)
	if err != nil {
		return nil, err
	}
	if tx.Tags == nil {
		tx.Tags = []uuid.UUID{}
	}
	return &tx, nil
}

func (r *TransactionRepository) UpdateTags(ctx context.Context, id uuid.UUID, tagIDs []uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `DELETE FROM transaction_tags WHERE transaction_id = $1`, id)
	if err != nil {
		return err
	}

	for _, tagID := range tagIDs {
		_, err = tx.Exec(ctx, `INSERT INTO transaction_tags (transaction_id, tag_id) VALUES ($1, $2)`, id, tagID)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *TransactionRepository) FindAllTagged(ctx context.Context, familyID *uuid.UUID) ([]models.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	whereClause := ""
	args := []interface{}{}
	if familyID != nil {
		whereClause = "WHERE t.family_id = $1"
		args = append(args, *familyID)
	}

	query := `
		SELECT t.id, t.fitid, t.type, t.date_posted, t.amount, t.name, t.memo, t.account_id, t.family_id, t.created_by, t.imported_at,
		       array_agg(tt.tag_id)
		FROM transactions t
		INNER JOIN transaction_tags tt ON t.id = tt.transaction_id
		` + whereClause + `
		GROUP BY t.id
	`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []models.Transaction
	for rows.Next() {
		var tx models.Transaction
		if err := rows.Scan(&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo, &tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt, &tx.Tags); err != nil {
			return nil, err
		}
		if tx.Tags == nil {
			tx.Tags = []uuid.UUID{}
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func (r *TransactionRepository) FindAllUntagged(ctx context.Context, familyID uuid.UUID) ([]models.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	query := `
		SELECT t.id, t.fitid, t.type, t.date_posted, t.amount, t.name, t.memo, t.account_id, t.family_id, t.created_by, t.imported_at
		FROM transactions t
		LEFT JOIN transaction_tags tt ON t.id = tt.transaction_id
		WHERE t.family_id = $1 AND tt.tag_id IS NULL
	`
	rows, err := r.pool.Query(ctx, query, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []models.Transaction
	for rows.Next() {
		var tx models.Transaction
		if err := rows.Scan(&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo, &tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt); err != nil {
			return nil, err
		}
		tx.Tags = []uuid.UUID{}
		txs = append(txs, tx)
	}
	return txs, nil
}

func (r *TransactionRepository) FindByIDAndFamily(ctx context.Context, id, familyID uuid.UUID) (*models.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	query := `
		SELECT t.id, t.fitid, t.type, t.date_posted, t.amount, t.name, t.memo, t.account_id, t.family_id, t.created_by, t.imported_at,
		       COALESCE(array_agg(tt.tag_id) FILTER (WHERE tt.tag_id IS NOT NULL), '{}')
		FROM transactions t
		LEFT JOIN transaction_tags tt ON t.id = tt.transaction_id
		WHERE t.id = $1 AND t.family_id = $2
		GROUP BY t.id
	`
	var tx models.Transaction
	err := r.pool.QueryRow(ctx, query, id, familyID).Scan(&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo, &tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt, &tx.Tags)
	if err != nil {
		return nil, err
	}
	if tx.Tags == nil {
		tx.Tags = []uuid.UUID{}
	}
	return &tx, nil
}
