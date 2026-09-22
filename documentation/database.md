# Guia de Banco de Dados, Migrations e Testes — Finager

Este documento descreve como o schema do PostgreSQL é gerenciado no projeto e como criar novas migrações e testes de integração de forma consistente.

---

## 1. Sincronização Obrigatória do Schema (Os 3 Locais)

O schema do banco vive em **três lugares complementares**. Qualquer alteração de DDL (novas tabelas, colunas, chaves estrangeiras ou índices) **deve** ser refletida nos três:

1. **`internal/migrations/NNN_nome.go` (Migração Oficial da Aplicação):**
   - É a trilha real de migração executada em produção e desenvolvimento via `make migrate`.
   - Auto-registrada através de `func init() { Register(&MNNNNome{}) }`.
   - Ordenada lexicograficamente pelo retorno de `ID()`.

2. **`internal/database/schema.sql` (Script de Init do Testcontainers):**
   - Utilizado **exclusivamente** pelos testes de integração automatizados.
   - O container Docker de testes sobe executando este arquivo como init script. Se uma coluna nova não estiver aqui, os testes de integração irão falhar com erro de coluna inexistente.

3. **`repository.Container.EnsureIndexes` (`internal/repository/container.go`):**
   - Bloco idempotente de queries DDL executado:
     - No boot do servidor HTTP (`cmd/api/main.go`).
     - No início de todo teste de integração (`repos.EnsureIndexes(ctx)`).
   - Use comandos com salvaguardas idempotentes (`ADD COLUMN IF NOT EXISTS`, `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`).

---

## 2. Passo a Passo: Como Criar uma Nova Migration

1. Verifique a última migração existente em `internal/migrations/` para obter o próximo número sequencial (ex: `008_...`).
2. Crie o arquivo `internal/migrations/NNN_descricao.go` seguindo o template:

```go
package migrations

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type M008Descricao struct{}

func (m *M008Descricao) ID() string          { return "008_descricao" }
func (m *M008Descricao) Description() string { return "Adiciona campo X à tabela Y" }

func (m *M008Descricao) Up(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
		ALTER TABLE tabela ADD COLUMN IF NOT EXISTS campo_x TEXT;
	`)
	return err
}

func init() {
	Register(&M008Descricao{})
}
```

3. Adicione o mesmo DDL em `internal/database/schema.sql`.
4. Adicione a query idempotente dentro do slice `queries` em `EnsureIndexes` (`internal/repository/container.go`).

---

## 3. Comandos Úteis de Migração

```bash
make migrate          # Aplica todas as migrações pendentes
make migrate-status   # Exibe a tabela com o status (APPLIED ou PENDING) de cada migração
make fresh            # Reinicia o ambiente Docker limpando o volume do PostgreSQL
```

---

## 4. Testes de Integração com Testcontainers

- **Localização dos testes:** Pacotes de teste devem ficar obrigatoriamente a exatamente dois níveis de profundidade (`internal/<pacote>/`, como `internal/handlers/` ou `internal/repository/`). O helper `testutil.SetupPostgresContainer(t)` resolve o caminho do `schema.sql` relativamente como `../../internal/database/schema.sql`.
- **Pré-requisito:** Docker daemon ativo na máquina local.
- **Padrão de inicialização:**

```go
func TestMinhaFuncionalidade(t *testing.T) {
	ctx := context.Background()
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()

	repos := repository.New(db)
	if err := repos.EnsureIndexes(ctx); err != nil {
		t.Fatalf("falha ao rodar EnsureIndexes: %v", err)
	}

	// Executar asserções...
}
```

- **Isolamento de Tenancy:** Todo novo teste de handler deve criar pelo menos duas famílias distintas e testar que a Família B recebe `404` ou `403` ao tentar acessar/modificar recursos criados pela Família A.
