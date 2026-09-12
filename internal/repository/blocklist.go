package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BlocklistRepository struct {
	pool *pgxpool.Pool
}

func NewBlocklistRepository(pool *pgxpool.Pool) *BlocklistRepository {
	return &BlocklistRepository{pool: pool}
}

func (r *BlocklistRepository) EnsureIndexes(ctx context.Context) error {
	return nil
}

func (r *BlocklistRepository) Add(ctx context.Context, hash string, expiresAt time.Time) error {
	query := `INSERT INTO blocklist (token_hash, expires_at) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err := r.pool.Exec(ctx, query, hash, expiresAt)
	return err
}

func (r *BlocklistRepository) Exists(ctx context.Context, hash string) (bool, error) {
	query := `SELECT 1 FROM blocklist WHERE token_hash = $1`
	var i int
	err := r.pool.QueryRow(ctx, query, hash).Scan(&i)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *BlocklistRepository) DeleteExpired(ctx context.Context) error {
	query := `DELETE FROM blocklist WHERE expires_at < now()`
	_, err := r.pool.Exec(ctx, query)
	return err
}
