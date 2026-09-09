package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

type AccountRepository struct {
	pool *pgxpool.Pool
}

func NewAccountRepository(pool *pgxpool.Pool) *AccountRepository {
	return &AccountRepository{pool: pool}
}

func (r *AccountRepository) Create(ctx context.Context, account *models.Account) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO accounts (name, institution, family_id, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`
	err := r.pool.QueryRow(ctx, query, account.Name, account.Institution, account.FamilyID, account.CreatedBy).Scan(
		&account.ID, &account.CreatedAt, &account.UpdatedAt,
	)
	if err != nil {
		return err
	}

	for _, userID := range account.AllowedUsers {
		_, err = r.pool.Exec(ctx, `INSERT INTO account_allowed_users (account_id, user_id) VALUES ($1, $2)`, account.ID, userID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *AccountRepository) FindVisibleAccounts(ctx context.Context, familyID, userID uuid.UUID) ([]*models.Account, error) {
	query := `
		SELECT a.id, a.name, a.institution, a.family_id, a.created_by, a.created_at, a.updated_at
		FROM accounts a
		LEFT JOIN account_allowed_users aau ON a.id = aau.account_id
		WHERE a.family_id = $1
		  AND (aau.user_id IS NULL OR aau.user_id = $2)
		GROUP BY a.id
	`
	rows, err := r.pool.Query(ctx, query, familyID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*models.Account
	for rows.Next() {
		var acc models.Account
		if err := rows.Scan(&acc.ID, &acc.Name, &acc.Institution, &acc.FamilyID, &acc.CreatedBy, &acc.CreatedAt, &acc.UpdatedAt); err != nil {
			return nil, err
		}
		accounts = append(accounts, &acc)
	}

	for _, acc := range accounts {
		rows, err := r.pool.Query(ctx, `SELECT user_id FROM account_allowed_users WHERE account_id = $1`, acc.ID)
		if err == nil {
			for rows.Next() {
				var uID uuid.UUID
				rows.Scan(&uID)
				acc.AllowedUsers = append(acc.AllowedUsers, uID)
			}
			rows.Close()
		}
	}

	if accounts == nil {
		accounts = []*models.Account{}
	}
	return accounts, nil
}

func (r *AccountRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Account, error) {
	query := `SELECT id, name, institution, family_id, created_by, created_at, updated_at FROM accounts WHERE id = $1`
	var acc models.Account
	err := r.pool.QueryRow(ctx, query, id).Scan(&acc.ID, &acc.Name, &acc.Institution, &acc.FamilyID, &acc.CreatedBy, &acc.CreatedAt, &acc.UpdatedAt)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT user_id FROM account_allowed_users WHERE account_id = $1`, acc.ID)
	if err == nil {
		for rows.Next() {
			var uID uuid.UUID
			rows.Scan(&uID)
			acc.AllowedUsers = append(acc.AllowedUsers, uID)
		}
		rows.Close()
	}

	if acc.AllowedUsers == nil {
		acc.AllowedUsers = []uuid.UUID{}
	}
	return &acc, nil
}

func (r *AccountRepository) Update(ctx context.Context, account *models.Account) error {
	query := `UPDATE accounts SET name = $1, institution = $2, updated_at = now() WHERE id = $3 AND family_id = $4`
	_, err := r.pool.Exec(ctx, query, account.Name, account.Institution, account.ID, account.FamilyID)
	if err != nil {
		return err
	}

	_, err = r.pool.Exec(ctx, `DELETE FROM account_allowed_users WHERE account_id = $1`, account.ID)
	if err != nil {
		return err
	}

	for _, userID := range account.AllowedUsers {
		_, err = r.pool.Exec(ctx, `INSERT INTO account_allowed_users (account_id, user_id) VALUES ($1, $2)`, account.ID, userID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *AccountRepository) Delete(ctx context.Context, id, familyID uuid.UUID) error {
	query := `DELETE FROM accounts WHERE id = $1 AND family_id = $2`
	_, err := r.pool.Exec(ctx, query, id, familyID)
	return err
}
