package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PasswordResetRepository persiste tokens de redefinição de senha (somente o hash).
type PasswordResetRepository struct {
	pool *pgxpool.Pool
}

func NewPasswordResetRepository(pool *pgxpool.Pool) *PasswordResetRepository {
	return &PasswordResetRepository{pool: pool}
}

// Create grava um novo token para o usuário num único statement: apaga os tokens
// anteriores do MESMO usuário (só o último pedido vale) e os expirados de qualquer
// usuário (limpeza oportunista), e insere o novo.
func (r *PasswordResetRepository) Create(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error {
	query := `WITH del AS (
			DELETE FROM password_reset_tokens WHERE user_id = $1 OR expires_at < now()
		)
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`
	_, err := r.pool.Exec(ctx, query, userID, tokenHash, expiresAt)
	return err
}

// Consume marca o token como usado e devolve o dono, num único UPDATE atômico:
// só vence se o token existir, não tiver sido usado e não estiver expirado.
// Caso contrário retorna pgx.ErrNoRows. Dois consumos concorrentes nunca ambos vencem.
func (r *PasswordResetRepository) Consume(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	query := `UPDATE password_reset_tokens SET used_at = now()
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING user_id`
	var userID uuid.UUID
	err := r.pool.QueryRow(ctx, query, tokenHash).Scan(&userID)
	return userID, err
}
