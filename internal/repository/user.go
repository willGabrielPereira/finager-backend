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
	query := `SELECT id, login, email, password_hash, family_id, onboarding_completed, onboarding_step, role, last_login_at, created_at, updated_at FROM users WHERE id = $1`
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Login, &email, &user.PasswordHash, &user.FamilyID, &user.OnboardingCompleted, &user.OnboardingStep, &user.Role, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
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

// CountByPlan retorna a contagem de usuários agrupados pelo plano da família a que pertencem.
func (r *UserRepository) CountByPlan(ctx context.Context) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT families.plan, COUNT(*)
		FROM users
		JOIN families ON users.family_id = families.id
		GROUP BY families.plan
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var plan string
		var count int
		if err := rows.Scan(&plan, &count); err != nil {
			return nil, err
		}
		result[plan] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// UserActivity representa dados agregados de atividade de um usuário (último login e última transação da família).
type UserActivity struct {
	UserID            uuid.UUID  `json:"user_id"`
	Login             string     `json:"login"`
	Email             string     `json:"email"`
	FamilyID          uuid.UUID  `json:"family_id"`
	LastLoginAt       *time.Time `json:"last_login_at,omitempty"`
	LastTransactionAt *time.Time `json:"last_transaction_at,omitempty"`
}

// ListActivity retorna usuários paginados com dados de último login e última transação da família,
// ordenados pela atividade mais recente (o maior entre last_login_at e last_transaction_at).
func (r *UserRepository) ListActivity(ctx context.Context, page, limit int) ([]UserActivity, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT u.id, u.login, u.email, u.family_id, u.last_login_at, lt.last_transaction_at
		FROM users u
		LEFT JOIN LATERAL (
			SELECT MAX(t.date_posted) AS last_transaction_at
			FROM transactions t
			WHERE t.family_id = u.family_id
		) lt ON true
		ORDER BY GREATEST(u.last_login_at, lt.last_transaction_at) DESC NULLS LAST
		LIMIT $1 OFFSET $2
	`
	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var activities []UserActivity
	for rows.Next() {
		var a UserActivity
		var email *string
		if err := rows.Scan(&a.UserID, &a.Login, &email, &a.FamilyID, &a.LastLoginAt, &a.LastTransactionAt); err != nil {
			return nil, 0, err
		}
		if email != nil {
			a.Email = *email
		}
		activities = append(activities, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return activities, total, nil
}

// UpdateRole atualiza o papel (role) do usuário.
func (r *UserRepository) UpdateRole(ctx context.Context, userID uuid.UUID, role string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE users SET role = $1, updated_at = now() WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, role, userID)
	return err
}

// UpdateLastLogin atualiza o timestamp de último login do usuário para agora.
func (r *UserRepository) UpdateLastLogin(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `UPDATE users SET last_login_at = now() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, userID)
	return err
}

// ListByRole retorna os usuários cujo role esteja entre os informados.
func (r *UserRepository) ListByRole(ctx context.Context, roles []string) ([]models.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT id, login, email, password_hash, family_id, onboarding_completed, onboarding_step, role, last_login_at, created_at, updated_at
		FROM users
		WHERE role = ANY($1)
	`
	rows, err := r.pool.Query(ctx, query, roles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		var email *string
		if err := rows.Scan(
			&user.ID, &user.Login, &email, &user.PasswordHash, &user.FamilyID, &user.OnboardingCompleted, &user.OnboardingStep, &user.Role, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if email != nil {
			user.Email = *email
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

// CountAdmins retorna o número de usuários com role 'admin'.
func (r *UserRepository) CountAdmins(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count)
	return count, err
}
