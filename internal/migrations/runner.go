package migrations

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Migration interface {
	ID() string
	Description() string
	Up(ctx context.Context, pool *pgxpool.Pool) error
}

type record struct {
	ID          string    `db:"id"`
	Description string    `db:"description"`
	AppliedAt   time.Time `db:"applied_at"`
}

type Runner struct {
	pool       *pgxpool.Pool
	migrations []Migration
}

func NewRunner(pool *pgxpool.Pool, migrations []Migration) *Runner {
	sorted := make([]Migration, len(migrations))
	copy(sorted, migrations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID() < sorted[j].ID()
	})
	return &Runner{pool: pool, migrations: sorted}
}

func (r *Runner) initTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS migrations (
			id VARCHAR(255) PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TIMESTAMP NOT NULL DEFAULT NOW()
		);
	`
	_, err := r.pool.Exec(ctx, query)
	return err
}

func (r *Runner) appliedSet(ctx context.Context) (map[string]bool, error) {
	if err := r.initTable(ctx); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, "SELECT id FROM migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		applied[id] = true
	}
	return applied, nil
}

func (r *Runner) Status(ctx context.Context) error {
	applied, err := r.appliedSet(ctx)
	if err != nil {
		return fmt.Errorf("migrations: falha ao ler estado aplicado: %w", err)
	}

	fmt.Printf("\n%-10s  %-40s  %s\n", "STATUS", "ID", "DESCRIÇÃO")
	fmt.Println("─────────────────────────────────────────────────────────────────────")
	for _, m := range r.migrations {
		status := "PENDING"
		if applied[m.ID()] {
			status = "APPLIED"
		}
		fmt.Printf("%-10s  %-40s  %s\n", status, m.ID(), m.Description())
	}
	fmt.Println("─────────────────────────────────────────────────────────────────────")
	pending := 0
	for _, m := range r.migrations {
		if !applied[m.ID()] {
			pending++
		}
	}
	fmt.Printf("\n%d pendente(s), %d aplicada(s).\n\n", pending, len(r.migrations)-pending)
	return nil
}

func (r *Runner) Up(ctx context.Context) error {
	applied, err := r.appliedSet(ctx)
	if err != nil {
		return fmt.Errorf("migrations: falha ao ler estado aplicado: %w", err)
	}

	pending := 0
	for _, m := range r.migrations {
		if applied[m.ID()] {
			log.Printf("  [SKIP]    %s", m.ID())
			continue
		}

		log.Printf("  [RUNNING] %s — %s", m.ID(), m.Description())

		if err := m.Up(ctx, r.pool); err != nil {
			return fmt.Errorf("migration %q falhou: %w", m.ID(), err)
		}

		_, err := r.pool.Exec(ctx, "INSERT INTO migrations (id, description, applied_at) VALUES ($1, $2, $3)", m.ID(), m.Description(), time.Now().UTC())
		if err != nil {
			return fmt.Errorf("migration %q: falha ao registrar conclusão no banco: %w", m.ID(), err)
		}

		log.Printf("  [DONE]    %s", m.ID())
		pending++
	}

	if pending == 0 {
		log.Println("Nenhuma migration pendente. Banco de dados atualizado.")
	} else {
		log.Printf("%d migration(s) aplicada(s) com sucesso.", pending)
	}
	return nil
}
