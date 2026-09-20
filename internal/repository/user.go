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
	query := `SELECT id, login, email, password_hash, family_id, onboarding_completed, onboarding_step, created_at, updated_at FROM users WHERE lower(login) = lower($1)`
	err := r.pool.QueryRow(ctx, query, login).Scan(
		&user.ID, &user.Login, &email, &user.PasswordHash, &user.FamilyID, &user.OnboardingCompleted, &user.OnboardingStep, &user.CreatedAt, &user.UpdatedAt,
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
	query := `SELECT id, login, email, password_hash, family_id, onboarding_completed, onboarding_step, created_at, updated_at FROM users WHERE lower(email) = lower($1)`
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Login, &scannedEmail, &user.PasswordHash, &user.FamilyID, &user.OnboardingCompleted, &user.OnboardingStep, &user.CreatedAt, &user.UpdatedAt,
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
		SELECT id, login, email, password_hash, family_id, onboarding_completed, onboarding_step, created_at, updated_at 
		FROM users 
		WHERE lower(login) = lower($1) OR lower(email) = lower($1)
		LIMIT 1
	`
	err := r.pool.QueryRow(ctx, query, identifier).Scan(
		&user.ID, &user.Login, &scannedEmail, &user.PasswordHash, &user.FamilyID, &user.OnboardingCompleted, &user.OnboardingStep, &user.CreatedAt, &user.UpdatedAt,
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
	query := `SELECT id, login, email, password_hash, family_id, onboarding_completed, onboarding_step, created_at, updated_at FROM users WHERE id = $1`
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Login, &email, &user.PasswordHash, &user.FamilyID, &user.OnboardingCompleted, &user.OnboardingStep, &user.CreatedAt, &user.UpdatedAt,
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

// UpdateOnboarding atualiza o status e a etapa do onboarding do usuário.
func (r *UserRepository) UpdateOnboarding(ctx context.Context, userID uuid.UUID, completed bool, step int) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE users SET onboarding_completed = $1, onboarding_step = $2, updated_at = now() WHERE id = $3`
	_, err := r.pool.Exec(ctx, query, completed, step, userID)
	return err
}

// DeleteAccount executa a exclusão de conta em conformidade com o direito à eliminação da LGPD (Art. 18).
// Remove os dados pessoais do usuário. Se o usuário for a última conta vinculada à família, remove em cascata
// transações, contas, tags, mapeamentos, convites, usuários e a própria família.
// Se houver outros membros na família, transfere a autoria das contas/transações para outro membro e desvincula o usuário.
func (r *UserRepository) DeleteAccount(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 1. Localiza a família do usuário
	var familyID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT family_id FROM users WHERE id = $1`, userID).Scan(&familyID)
	if err != nil {
		return err
	}

	// 2. Conta outros usuários ativos vinculados à mesma família
	var otherUsersCount int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE family_id = $1 AND id != $2`, familyID, userID).Scan(&otherUsersCount)
	if err != nil {
		return err
	}

	// 3. Remove tokens de refresh e vínculos de permissão do usuário
	if _, err := tx.Exec(ctx, `DELETE FROM refresh_tokens WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM account_allowed_users WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE family_invites SET used_by = NULL WHERE used_by = $1`, userID); err != nil {
		return err
	}

	if otherUsersCount == 0 {
		// O usuário é a ÚLTIMA conta vinculada à família: exclusão completa da família e todos os dados
		if _, err := tx.Exec(ctx, `DELETE FROM transactions WHERE family_id = $1`, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM accounts WHERE family_id = $1`, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM tags WHERE family_id = $1`, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM merchant_mappings WHERE family_id = $1`, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM classifier_states WHERE family_id = $1`, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM family_invites WHERE family_id = $1`, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM family_members WHERE family_id = $1`, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM families WHERE id = $1`, familyID); err != nil {
			return err
		}
	} else {
		// Há outros membros na família: elege outro membro ativo para assumir autoria dos registros
		var successorID uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id FROM users WHERE family_id = $1 AND id != $2 LIMIT 1`, familyID, userID).Scan(&successorID)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE accounts SET created_by = $1 WHERE created_by = $2 AND family_id = $3`, successorID, userID, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE transactions SET created_by = $1 WHERE created_by = $2 AND family_id = $3`, successorID, userID, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM family_invites WHERE created_by = $1 AND family_id = $2`, userID, familyID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM family_members WHERE family_id = $1 AND user_id = $2`, familyID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}


