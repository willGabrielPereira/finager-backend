package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

type RefreshTokenRepository struct {
	pool *pgxpool.Pool
}

func NewRefreshTokenRepository(pool *pgxpool.Pool) *RefreshTokenRepository {
	return &RefreshTokenRepository{pool: pool}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, token *models.RefreshToken) error {
	query := `INSERT INTO refresh_tokens (user_id, token_hash, expires_at, revoked) VALUES ($1, $2, $3, $4) RETURNING id`
	return r.pool.QueryRow(ctx, query, token.UserID, token.TokenHash, token.ExpiresAt, token.Revoked).Scan(&token.ID)
}

// RevokeByHash revoga o refresh token ativo (não revogado e não expirado) com o
// hash informado e o retorna, num único statement atômico. Se o token não
// existir, já estiver revogado ou expirado, retorna pgx.ErrNoRows. Por ser
// atômico, dois Refresh concorrentes com o mesmo token nunca ambos vencem.
func (r *RefreshTokenRepository) RevokeByHash(ctx context.Context, hash string) (*models.RefreshToken, error) {
	query := `UPDATE refresh_tokens SET revoked = true
		WHERE token_hash = $1 AND NOT revoked AND expires_at > now()
		RETURNING id, user_id, token_hash, expires_at, revoked`
	var t models.RefreshToken
	err := r.pool.QueryRow(ctx, query, hash).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.Revoked)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *RefreshTokenRepository) RevokeAllByUser(ctx context.Context, userID uuid.UUID) error {
	query := `UPDATE refresh_tokens SET revoked = true WHERE user_id = $1`
	_, err := r.pool.Exec(ctx, query, userID)
	return err
}

func (r *RefreshTokenRepository) DeleteExpired(ctx context.Context) error {
	query := `DELETE FROM refresh_tokens WHERE expires_at < now()`
	_, err := r.pool.Exec(ctx, query)
	return err
}
