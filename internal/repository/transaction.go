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

		status := tx.Status
		if status == "" {
			status = models.TxStatusPosted
		}
		source := tx.Source
		if source == "" {
			source = models.TxSourceOFX
		}

		query := `
			INSERT INTO transactions (
				fitid, type, date_posted, amount, name, memo, account_id, family_id, created_by, imported_at,
				manually_tagged, status, is_transfer, destination_account_id, source
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			ON CONFLICT (fitid, account_id, family_id) DO UPDATE
			SET name = EXCLUDED.name,
			    memo = EXCLUDED.memo
			WHERE (transactions.name = '' OR transactions.name IS NULL) AND EXCLUDED.name != ''
			RETURNING id
		`
		err := r.pool.QueryRow(
			ctx, query,
			tx.FITID, tx.Type, tx.DatePosted, tx.Amount, tx.Name, tx.Memo,
			tx.AccountID, tx.FamilyID, tx.CreatedBy, time.Now(),
			tx.ManuallyTagged, status, tx.IsTransfer, tx.DestinationAccountID, source,
		).Scan(&id)

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

// Create cria manualmente uma transação (avulsa, planejada ou pendente de conciliação)
func (r *TransactionRepository) Create(ctx context.Context, tx *models.Transaction) (*models.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if tx.ID == uuid.Nil {
		tx.ID = uuid.New()
	}
	if tx.FITID == "" {
		tx.FITID = "MANUAL_" + tx.ID.String()
	}
	if tx.Status == "" {
		tx.Status = models.TxStatusPosted
	}
	if tx.Source == "" {
		tx.Source = models.TxSourceManual
	}
	tx.ImportedAt = time.Now()

	dbTx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer dbTx.Rollback(ctx)

	query := `
		INSERT INTO transactions (
			id, fitid, type, date_posted, amount, name, memo, account_id, family_id, created_by, imported_at,
			manually_tagged, status, is_transfer, destination_account_id, source
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id, imported_at
	`
	err = dbTx.QueryRow(ctx, query,
		tx.ID, tx.FITID, tx.Type, tx.DatePosted, tx.Amount, tx.Name, tx.Memo,
		tx.AccountID, tx.FamilyID, tx.CreatedBy, tx.ImportedAt,
		tx.ManuallyTagged, tx.Status, tx.IsTransfer, tx.DestinationAccountID, tx.Source,
	).Scan(&tx.ID, &tx.ImportedAt)
	if err != nil {
		return nil, err
	}

	for _, tagID := range tx.Tags {
		_, err = dbTx.Exec(ctx, `INSERT INTO transaction_tags (transaction_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, tx.ID, tagID)
		if err != nil {
			return nil, err
		}
	}

	if err := dbTx.Commit(ctx); err != nil {
		return nil, err
	}

	return tx, nil
}

// Delete remove uma transação garantindo isolamento da família
func (r *TransactionRepository) Delete(ctx context.Context, id, familyID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd, err := r.pool.Exec(ctx, `DELETE FROM transactions WHERE id = $1 AND family_id = $2`, id, familyID)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// FindReconciliationMatch procura uma transação manual ou planejada compatível para conciliação
func (r *TransactionRepository) FindReconciliationMatch(ctx context.Context, familyID, accountID uuid.UUID, amount float64, datePosted time.Time) (*models.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	query := `
		SELECT t.id, t.fitid, t.type, t.date_posted, t.amount, t.name, t.memo, t.account_id, t.family_id, t.created_by, t.imported_at,
		       t.manually_tagged, t.status, t.is_transfer, t.destination_account_id, t.source,
		       COALESCE(array_agg(tt.tag_id) FILTER (WHERE tt.tag_id IS NOT NULL), '{}')
		FROM transactions t
		LEFT JOIN transaction_tags tt ON t.id = tt.transaction_id
		WHERE t.family_id = $1 AND t.account_id = $2
		  AND t.status IN ('PENDING_RECONCILIATION', 'PLANNED')
		  AND ABS(t.amount - $3) < 0.01
		  AND t.date_posted BETWEEN ($4 - INTERVAL '4 days') AND ($4 + INTERVAL '4 days')
		GROUP BY t.id
		ORDER BY ABS(EXTRACT(EPOCH FROM (t.date_posted - $4))) ASC
		LIMIT 1
	`
	var tx models.Transaction
	err := r.pool.QueryRow(ctx, query, familyID, accountID, amount, datePosted).Scan(
		&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo,
		&tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt,
		&tx.ManuallyTagged, &tx.Status, &tx.IsTransfer, &tx.DestinationAccountID, &tx.Source,
		&tx.Tags,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if tx.Tags == nil {
		tx.Tags = []uuid.UUID{}
	}
	return &tx, nil
}

// Reconcile unifica o lançamento manual com o FITID e a data real do banco
func (r *TransactionRepository) Reconcile(ctx context.Context, id, familyID uuid.UUID, fitid string, actualDate time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	query := `
		UPDATE transactions
		SET fitid = $1, status = 'RECONCILED', date_posted = $2
		WHERE id = $3 AND family_id = $4
	`
	_, err := r.pool.Exec(ctx, query, fitid, actualDate, id, familyID)
	return err
}

// ApplyTagToSimilar propaga uma tag para outras transações de nome semelhante
func (r *TransactionRepository) ApplyTagToSimilar(ctx context.Context, familyID uuid.UUID, pattern string, tagID uuid.UUID, includeManuallyTagged bool) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return 0, nil
	}

	dbTx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer dbTx.Rollback(ctx)

	whereClause := "family_id = $1 AND (name ILIKE '%' || $2 || '%' OR memo ILIKE '%' || $2 || '%')"
	if !includeManuallyTagged {
		whereClause += " AND manually_tagged = false"
	}

	rows, err := dbTx.Query(ctx, "SELECT id FROM transactions WHERE "+whereClause, familyID, pattern)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var txIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		txIDs = append(txIDs, id)
	}

	if len(txIDs) == 0 {
		return 0, nil
	}

	for _, id := range txIDs {
		_, _ = dbTx.Exec(ctx, "DELETE FROM transaction_tags WHERE transaction_id = $1", id)
		_, err = dbTx.Exec(ctx, "INSERT INTO transaction_tags (transaction_id, tag_id) VALUES ($1, $2)", id, tagID)
		if err != nil {
			return 0, err
		}
		_, err = dbTx.Exec(ctx, "UPDATE transactions SET manually_tagged = true WHERE id = $1", id)
		if err != nil {
			return 0, err
		}
	}

	if err := dbTx.Commit(ctx); err != nil {
		return 0, err
	}

	return int64(len(txIDs)), nil
}

type ListFilter struct {
	FamilyID          uuid.UUID
	AllowedAccountIDs []uuid.UUID
	AccountIDs        []uuid.UUID // Filtro de seleção múltipla de contas
	TagID             *uuid.UUID
	TagIDs            []uuid.UUID // Filtro de seleção múltipla de tags
	Type              string
	Status            string
	DateFrom          time.Time
	DateTo            time.Time
	AmountMin         *float64
	AmountMax         *float64
	Search            string
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

	// Determina as contas a filtrar, garantindo que o usuário só acesse contas permitidas
	effectiveAccountIDs := f.AllowedAccountIDs
	if len(f.AccountIDs) > 0 {
		var filtered []uuid.UUID
		allowedMap := make(map[uuid.UUID]bool)
		for _, id := range f.AllowedAccountIDs {
			allowedMap[id] = true
		}
		for _, id := range f.AccountIDs {
			if allowedMap[id] {
				filtered = append(filtered, id)
			}
		}
		effectiveAccountIDs = filtered
	}

	if len(effectiveAccountIDs) > 0 {
		var placeholders []string
		for _, id := range effectiveAccountIDs {
			placeholders = append(placeholders, fmt.Sprintf("$%d", argIdx))
			args = append(args, id)
			argIdx++
		}
		whereClauses = append(whereClauses, fmt.Sprintf("t.account_id IN (%s)", strings.Join(placeholders, ", ")))
	} else {
		whereClauses = append(whereClauses, "1=0")
	}

	if len(f.TagIDs) > 0 {
		var tagPlaceholders []string
		for _, tID := range f.TagIDs {
			tagPlaceholders = append(tagPlaceholders, fmt.Sprintf("$%d", argIdx))
			args = append(args, tID)
			argIdx++
		}
		whereClauses = append(whereClauses, fmt.Sprintf("EXISTS (SELECT 1 FROM transaction_tags tt WHERE tt.transaction_id = t.id AND tt.tag_id IN (%s))", strings.Join(tagPlaceholders, ", ")))
	} else if f.TagID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("EXISTS (SELECT 1 FROM transaction_tags tt WHERE tt.transaction_id = t.id AND tt.tag_id = $%d)", argIdx))
		args = append(args, *f.TagID)
		argIdx++
	}

	if f.Type != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("t.type = $%d", argIdx))
		args = append(args, f.Type)
		argIdx++
	}

	if f.Status != "" {
		if strings.EqualFold(f.Status, "UNTAGGED") {
			whereClauses = append(whereClauses, "NOT EXISTS (SELECT 1 FROM transaction_tags tt WHERE tt.transaction_id = t.id)")
		} else {
			whereClauses = append(whereClauses, fmt.Sprintf("t.status = $%d", argIdx))
			args = append(args, f.Status)
			argIdx++
		}
	}

	if f.Search != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("(t.name ILIKE $%d OR t.memo ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+f.Search+"%")
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
		       t.manually_tagged, t.status, t.is_transfer, t.destination_account_id, t.source,
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
		if err := rows.Scan(
			&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo,
			&tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt,
			&tx.ManuallyTagged, &tx.Status, &tx.IsTransfer, &tx.DestinationAccountID, &tx.Source,
			&tx.Tags,
		); err != nil {
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
		       t.manually_tagged, t.status, t.is_transfer, t.destination_account_id, t.source,
		       COALESCE(array_agg(tt.tag_id) FILTER (WHERE tt.tag_id IS NOT NULL), '{}')
		FROM transactions t
		LEFT JOIN transaction_tags tt ON t.id = tt.transaction_id
		WHERE t.id = $1
		GROUP BY t.id
	`
	var tx models.Transaction
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo,
		&tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt,
		&tx.ManuallyTagged, &tx.Status, &tx.IsTransfer, &tx.DestinationAccountID, &tx.Source,
		&tx.Tags,
	)
	if err != nil {
		return nil, err
	}
	if tx.Tags == nil {
		tx.Tags = []uuid.UUID{}
	}
	return &tx, nil
}

// UpdateTags altera as tags de uma transação e marca manually_tagged = true
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

	_, err = tx.Exec(ctx, `UPDATE transactions SET manually_tagged = true WHERE id = $1`, id)
	if err != nil {
		return err
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
		       t.manually_tagged, t.status, t.is_transfer, t.destination_account_id, t.source,
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
		if err := rows.Scan(
			&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo,
			&tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt,
			&tx.ManuallyTagged, &tx.Status, &tx.IsTransfer, &tx.DestinationAccountID, &tx.Source,
			&tx.Tags,
		); err != nil {
			return nil, err
		}
		if tx.Tags == nil {
			tx.Tags = []uuid.UUID{}
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func (r *TransactionRepository) FindAllUntagged(ctx context.Context, familyID uuid.UUID, includeManuallyTagged bool) ([]models.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	whereClause := "t.family_id = $1 AND tt.tag_id IS NULL"
	if includeManuallyTagged {
		whereClause = "t.family_id = $1"
	}

	query := `
		SELECT t.id, t.fitid, t.type, t.date_posted, t.amount, t.name, t.memo, t.account_id, t.family_id, t.created_by, t.imported_at,
		       t.manually_tagged, t.status, t.is_transfer, t.destination_account_id, t.source
		FROM transactions t
		LEFT JOIN transaction_tags tt ON t.id = tt.transaction_id
		WHERE ` + whereClause + `
		GROUP BY t.id
	`
	rows, err := r.pool.Query(ctx, query, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []models.Transaction
	for rows.Next() {
		var tx models.Transaction
		if err := rows.Scan(
			&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo,
			&tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt,
			&tx.ManuallyTagged, &tx.Status, &tx.IsTransfer, &tx.DestinationAccountID, &tx.Source,
		); err != nil {
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
		       t.manually_tagged, t.status, t.is_transfer, t.destination_account_id, t.source,
		       COALESCE(array_agg(tt.tag_id) FILTER (WHERE tt.tag_id IS NOT NULL), '{}')
		FROM transactions t
		LEFT JOIN transaction_tags tt ON t.id = tt.transaction_id
		WHERE t.id = $1 AND t.family_id = $2
		GROUP BY t.id
	`
	var tx models.Transaction
	err := r.pool.QueryRow(ctx, query, id, familyID).Scan(
		&tx.ID, &tx.FITID, &tx.Type, &tx.DatePosted, &tx.Amount, &tx.Name, &tx.Memo,
		&tx.AccountID, &tx.FamilyID, &tx.CreatedBy, &tx.ImportedAt,
		&tx.ManuallyTagged, &tx.Status, &tx.IsTransfer, &tx.DestinationAccountID, &tx.Source,
		&tx.Tags,
	)
	if err != nil {
		return nil, err
	}
	if tx.Tags == nil {
		tx.Tags = []uuid.UUID{}
	}
	return &tx, nil
}
