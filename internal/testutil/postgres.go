package testutil

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// SetupPostgresContainer inicia um PostgreSQL usando Testcontainers e retorna o Pool conectado e uma função de cleanup.
func SetupPostgresContainer(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()

	// Procura o schema.sql subindo os diretórios
	schemaPath, err := filepath.Abs("../../internal/database/schema.sql")
	if err != nil {
		t.Fatalf("Failed to resolve schema.sql path: %v", err)
	}

	pgContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithInitScripts(schemaPath),
		postgres.WithDatabase("finager_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("Failed to start Postgres container: %v", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get connection string: %v", err)
	}

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("Failed to connect to Postgres: %v", err)
	}

	cleanup := func() {
		pool.Close()
		if err := pgContainer.Terminate(context.Background()); err != nil {
			t.Fatalf("Failed to terminate container: %v", err)
		}
	}

	return pool, cleanup
}
