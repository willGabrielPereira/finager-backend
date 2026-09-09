package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// FamilyRepository encapsulates persistence operations for Family.
type FamilyRepository struct {
	pool *pgxpool.Pool
}

// NewFamilyRepository creates a FamilyRepository pointing at the correct database.
func NewFamilyRepository(pool *pgxpool.Pool) *FamilyRepository {
	return &FamilyRepository{pool: pool}
}

// Create inserts a new family document. Sets timestamps automatically.
func (r *FamilyRepository) Create(ctx context.Context, family *models.Family) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO families (name)
		VALUES ($1)
		RETURNING id, created_at, updated_at
	`
	err := r.pool.QueryRow(ctx, query, family.Name).Scan(&family.ID, &family.CreatedAt, &family.UpdatedAt)
	return err
}

// FindByID returns the family with the given UUID.
func (r *FamilyRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Family, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var family models.Family
	query := `SELECT id, name, created_at, updated_at FROM families WHERE id = $1`
	err := r.pool.QueryRow(ctx, query, id).Scan(&family.ID, &family.Name, &family.CreatedAt, &family.UpdatedAt)
	if err != nil {
		return nil, err
	}

	// Fetch member IDs
	membersQuery := `SELECT user_id FROM family_members WHERE family_id = $1`
	rows, err := r.pool.Query(ctx, membersQuery, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memberIDs []uuid.UUID
	for rows.Next() {
		var memberID uuid.UUID
		if err := rows.Scan(&memberID); err != nil {
			return nil, err
		}
		memberIDs = append(memberIDs, memberID)
	}
	family.MemberIDs = memberIDs

	return &family, nil
}

// FindByName returns the family with the given name.
func (r *FamilyRepository) FindByName(ctx context.Context, name string) (*models.Family, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var family models.Family
	query := `SELECT id, name, created_at, updated_at FROM families WHERE name = $1 LIMIT 1`
	err := r.pool.QueryRow(ctx, query, name).Scan(&family.ID, &family.Name, &family.CreatedAt, &family.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &family, nil
}

// AddMember appends a user ID to the family's member list (no-op if already present).
func (r *FamilyRepository) AddMember(ctx context.Context, familyID, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO family_members (family_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`
	_, err := r.pool.Exec(ctx, query, familyID, userID)
	if err != nil {
		return err
	}

	updateQuery := `UPDATE families SET updated_at = now() WHERE id = $1`
	_, err = r.pool.Exec(ctx, updateQuery, familyID)
	return err
}
