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
	var email *string
	query := `SELECT id, login, email, password_hash, family_id, created_at, updated_at FROM users WHERE lower(login) = lower($1)`
	err := r.pool.QueryRow(ctx, query, login).Scan(
		&user.ID, &user.Login, &email, &user.PasswordHash, &user.FamilyID, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if email != nil {
		user.Email = *email
	}
	return &user, nil
}

// FindByEmail returns the user with the given email, or pgx.ErrNoRows.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user models.User
	var scannedEmail *string
	query := `SELECT id, login, email, password_hash, family_id, created_at, updated_at FROM users WHERE lower(email) = lower($1)`
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Login, &scannedEmail, &user.PasswordHash, &user.FamilyID, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if scannedEmail != nil {
		user.Email = *scannedEmail
	}
	return &user, nil
}

// FindByLoginOrEmail returns the user matching login or email.
func (r *UserRepository) FindByLoginOrEmail(ctx context.Context, identifier string) (*models.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user models.User
	var scannedEmail *string
	query := `
		SELECT id, login, email, password_hash, family_id, created_at, updated_at 
		FROM users 
		WHERE lower(login) = lower($1) OR lower(email) = lower($1)
		LIMIT 1
	`
	err := r.pool.QueryRow(ctx, query, identifier).Scan(
		&user.ID, &user.Login, &scannedEmail, &user.PasswordHash, &user.FamilyID, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if scannedEmail != nil {
		user.Email = *scannedEmail
	}
	return &user, nil
}

// FindByID returns the user with the given UUID.
func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user models.User
	var email *string
	query := `SELECT id, login, email, password_hash, family_id, created_at, updated_at FROM users WHERE id = $1`
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Login, &email, &user.PasswordHash, &user.FamilyID, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if email != nil {
		user.Email = *email
	}
	return &user, nil
}

// Create inserts a new user. Sets CreatedAt and UpdatedAt automatically.
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var email *string
	if user.Email != "" {
		email = &user.Email
	}

	query := `
		INSERT INTO users (login, email, password_hash, family_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`
	err := r.pool.QueryRow(ctx, query, user.Login, email, user.PasswordHash, user.FamilyID).Scan(
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

// UpdateLogin atualiza o nome de usuário (login).
func (r *UserRepository) UpdateLogin(ctx context.Context, userID uuid.UUID, login string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE users SET login = $1, updated_at = now() WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, login, userID)
	return err
}

// UpdateEmail atualiza o e-mail do usuário.
func (r *UserRepository) UpdateEmail(ctx context.Context, userID uuid.UUID, email string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE users SET email = $1, updated_at = now() WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, email, userID)
	return err
}

