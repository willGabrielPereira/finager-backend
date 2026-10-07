# Guia de Banco de Dados, Migrations e Testes — Finager

Este documento descreve como o schema do PostgreSQL é gerenciado no projeto e como criar novas migrações e testes de integração de forma consistente.

---

## 1. Sincronização Obrigatória do Schema (Os 2 Locais)

O schema do banco vive em **dois lugares complementares**. Qualquer alteração de DDL (novas tabelas, colunas, chaves estrangeiras ou índices) **deve** ser refletida nos dois:

1. **`internal/migrations/NNN_nome.go` (Migração Oficial da Aplicação):**
   - É a trilha real de migração executada em produção e desenvolvimento via `make migrate`.
   - O `Dockerfile` já roda `finager-migrate up` antes de subir a API (`ENTRYPOINT`), então o schema de produção está sempre atualizado no boot.
   - Auto-registrada através de `func init() { Register(&MNNNNome{}) }`.
   - Ordenada lexicograficamente pelo retorno de `ID()`.

2. **`internal/database/schema.sql` (Script de Init do Testcontainers):**
   - Utilizado **exclusivamente** pelos testes de integração automatizados.
   - O container Docker de testes sobe executando este arquivo como init script. Se uma coluna nova não estiver aqui, os testes de integração irão falhar com erro de coluna inexistente.

> Havia um terceiro local, `repository.Container.EnsureIndexes`, que reexecutava a mesma DDL idempotente no boot e em cada teste — pura redundância, já que `make migrate` (produção) e `schema.sql` (testes) já cobriam tudo. Foi removido; não recrie esse padrão.

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

	// Executar asserções...
}
```

- **Isolamento de Tenancy:** Todo novo teste de handler deve criar pelo menos duas famílias distintas e testar que a Família B recebe `404` ou `403` ao tentar acessar/modificar recursos criados pela Família A.

## Acesso do suporte aos dados de uma família (dump)

- A família concede o acesso com prazo: `PUT /family/support-access` `{enabled, days}` (1–30, padrão 7) → `families.support_access_until`. Expirado/NULL = sem acesso.
- `GET /admin/families/{id}/dump?reason=...` (admin/moderator + elevação por senha) devolve um script psql: `schema.sql` embutido (DDL) + `COPY ... FROM stdin` por tabela, num snapshot REPEATABLE READ. `password_hash` sai como `REDACTED`; tokens, blocklist e convites não são exportados. Termina com `DumpEndMarker` (a CLI detecta truncamento).
- Cada dump grava `admin_audit_log` e avisa os membros por e-mail (`support_export.html`, ignora opt-out).
- CLI: `make support-dump FAMILY=<uuid> REASON="ticket 123" [OUT=dump.sql]` (env `FINAGER_API_URL`, `FINAGER_ADMIN_LOGIN`, opcional `FINAGER_ADMIN_PASSWORD`). Restaurar: `psql -d <banco_vazio> -f dump.sql`.
- Ao adicionar tabela com dados de família, inclua-a em `dumpTables` (`internal/repository/dump.go`).
