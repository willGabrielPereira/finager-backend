package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// UserRepository encapsulates persistence operations for User.
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository creates a UserRepository pointing at the correct database.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// EnsureIndexes is kept for interface compatibility (indexes created via schema.sql)
func (r *UserRepository) EnsureIndexes(ctx context.Context) error {
	return nil
}

// FindByLogin returns the user with the given login, or pgx.ErrNoRows.
func (r *UserRepository) FindByLogin(ctx context.Context, login string) (*models.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user models.User
	query := `SELECT id, login, password_hash, family_id, created_at, updated_at FROM users WHERE login = $1`
	err := r.pool.QueryRow(ctx, query, login).Scan(
		&user.ID, &user.Login, &user.PasswordHash, &user.FamilyID, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// FindByID returns the user with the given UUID.
func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user models.User
	query := `SELECT id, login, password_hash, family_id, created_at, updated_at FROM users WHERE id = $1`
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Login, &user.PasswordHash, &user.FamilyID, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Create inserts a new user. Sets CreatedAt and UpdatedAt automatically.
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO users (login, password_hash, family_id)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`
	err := r.pool.QueryRow(ctx, query, user.Login, user.PasswordHash, user.FamilyID).Scan(
		&user.ID, &user.CreatedAt, &user.UpdatedAt,
	)
	return err
}

// UpdatePassword sets a new bcrypt password hash for the given user.
func (r *UserRepository) UpdatePassword(ctx context.Context, userID uuid.UUID, newHash string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, newHash, userID)
	return err
}

// UpdateFamilyID reassigns the user to a different family.
// Used when a user joins an existing shared family.
func (r *UserRepository) UpdateFamilyID(ctx context.Context, userID, familyID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE users SET family_id = $1, updated_at = now() WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, familyID, userID)
	return err
}
