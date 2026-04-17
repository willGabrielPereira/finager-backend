// Package migrations fornece um runner leve e opinativo para migrações de banco de dados MongoDB.
//
// Filosofia:
//   - Cada migração tem um ID único e léxicamente ordenável (ex: "001_descricao").
//   - Migrações já aplicadas ficam registradas na collection "migrations" do próprio banco.
//   - O runner é idempotente: executá-lo N vezes tem o mesmo efeito que executá-lo uma vez.
//   - NUNCA reordene ou remova migrações do registry. Para reverter, adicione uma nova migração.
package migrations

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const migrationCollection = "migrations"

// Migration representa uma alteração versionada e controlada no banco de dados.
type Migration interface {
	// ID retorna o identificador único e ordenavelmente crescente da migração.
	// Convenção: "<NNN>_<descricao_snake_case>", ex: "001_tags_string_to_objectid".
	ID() string

	// Description retorna uma descrição humana do que a migração faz.
	Description() string

	// Up aplica a migração. Deve ser idempotente sempre que possível.
	Up(ctx context.Context, db *mongo.Database) error
}

// record persiste no banco o estado de uma migração já aplicada.
type record struct {
	ID          string    `bson:"_id"`
	Description string    `bson:"description"`
	AppliedAt   time.Time `bson:"applied_at"`
}

// Runner controla a execução ordenada e o estado das migrações.
type Runner struct {
	db         *mongo.Database
	migrations []Migration
}

// NewRunner cria um Runner com as migrações fornecidas.
// As migrações serão ordenadas pelo ID antes de qualquer execução.
func NewRunner(db *mongo.Database, migrations []Migration) *Runner {
	sorted := make([]Migration, len(migrations))
	copy(sorted, migrations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID() < sorted[j].ID()
	})
	return &Runner{db: db, migrations: sorted}
}

// appliedSet lê a collection de controle e retorna os IDs das migrações já aplicadas.
func (r *Runner) appliedSet(ctx context.Context) (map[string]bool, error) {
	cursor, err := r.db.Collection(migrationCollection).Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var records []record
	if err := cursor.All(ctx, &records); err != nil {
		return nil, err
	}

	applied := make(map[string]bool, len(records))
	for _, rec := range records {
		applied[rec.ID] = true
	}
	return applied, nil
}

// Status imprime o estado (APPLIED / PENDING) de cada migração registrada.
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

// Up aplica todas as migrações pendentes em ordem crescente de ID.
// Já aplicadas são puladas silenciosamente.
// Se uma migração falhar, a execução para imediatamente (fail-fast).
func (r *Runner) Up(ctx context.Context) error {
	col := r.db.Collection(migrationCollection)

	// Garante índice único no _id (idempotente).
	_, _ = col.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

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

		if err := m.Up(ctx, r.db); err != nil {
			return fmt.Errorf("migration %q falhou: %w", m.ID(), err)
		}

		rec := record{
			ID:          m.ID(),
			Description: m.Description(),
			AppliedAt:   time.Now().UTC(),
		}
		if _, err := col.InsertOne(ctx, rec); err != nil {
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
