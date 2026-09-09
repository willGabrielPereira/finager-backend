package database

import (
    "context"
    "log"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool connection pool.
type DB struct {
    Pool *pgxpool.Pool
}

// Connect establishes a connection pool to PostgreSQL.
func Connect(dsn string) (*DB, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    pool, err := pgxpool.New(ctx, dsn)
    if err != nil {
        return nil, err
    }

    if err := pool.Ping(ctx); err != nil {
        return nil, err
    }

    log.Printf("Connected to PostgreSQL")
    return &DB{Pool: pool}, nil
}

// Close closes the connection pool.
func (db *DB) Close() {
    db.Pool.Close()
}
