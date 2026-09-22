# AGENTS.md — Finager API

Go 1.25 REST API (backend only). Módulo: `github.com/willGabrielPereira/finager-backend`.
Stack: PostgreSQL 15 via `pgx/v5` (`pgxpool`), stdlib `net/http` (Go 1.22+ ServeMux), JWT, `ofxgo` para OFX, `swaggo/swag` para docs.

---

## Invariantes Críticas do Projeto (LEIA ANTES DE MODIFICAR CÓDIGO)

1. **Build Gotcha (Swagger obrigatório antes de compilar):**
   - O diretório `docs/` é ignorado pelo git, mas `cmd/api/main.go` importa `_ ".../docs"`.
   - Em clones novos ou após qualquer alteração em anotações Swagger nos handlers (`@Param`, `@Success`, `@Failure`, etc.), execute `make docs` (`swag init -g cmd/api/main.go --output docs`).
   - Sem gerar o Swagger, `go build ./...` e `go test ./cmd/...` irão **falhar**.

2. **Database Schema vive em TRÊS lugares (Mantenha sempre sincronizado):**
   - `internal/migrations/NNN_*.go`: Migração real auto-registrada via `init()` com `Register(&MNNN{})`.
   - `internal/database/schema.sql`: DDL utilizado como init script do container nos testes com Testcontainers.
   - `repository.Container.EnsureIndexes` (`internal/repository/container.go`): DDL idempotente (`IF NOT EXISTS`) executado no boot e nos testes de integração.

3. **Multi-Tenancy e Isolamento por Família:**
   - Todo dado pertence a uma família (`family_id`). O `middleware.Authenticate` injeta as claims no `context.Context`.
   - NUNCA consulte ou altere recursos filtrando apenas por `user_id` sem validar o `family_id`.
   - Todo novo handler deve ter teste de integração garantindo que uma família não consegue acessar nem alterar dados de outra família.

4. **Convenções e Estilo:**
   - Comentários no código e documentações são escritos em Português (pt-BR). Mantenha esse padrão.
   - Rotas vivem exclusivamente em `cmd/api/routes.go` usando padrões do Go 1.22+ (ex: `"GET /transactions/{id}"`).
   - Injeção de dependências é centralizada em `repository.Container`. Handlers recebem os repositórios necessários, nunca a pool de conexões direta.

---

## Comandos Essenciais

```bash
make docs                # Regenera Swagger em docs/ (obrigatório após editar anotações de handlers)
make run                 # Gera docs + sobe a API em http://localhost:8080
make migrate             # Executa migrações pendentes no banco local
make migrate-status      # Exibe status (APPLIED / PENDING) das migrações
make seed                # Popula usuários, família, contas e tags de teste (idempotente)
make seed-tags           # Popula apenas tags de sistema (cold-start limpo)
make test                # Executa toda a suíte de testes (requer Docker ativo)
go test ./internal/handlers -run TestTransactionIntegrationAndSecurity -v # Teste específico
```

---

## Diretrizes de Economia de Tokens e Eficiência para Agentes

- **Investigue cirurgicamente antes de editar:** Use `grep_search` para encontrar nomes de símbolos ou rotas. Não leia pastas inteiras nem arquivos completos sem necessidade.
- **Leitura em fatias:** Ao inspecionar arquivos grandes (`internal/handlers/transaction.go`, `schema.sql`), especifique intervalos de linhas (`StartLine`/`EndLine`).
- **Não duplique a lista de rotas:** Para consultar as rotas da API, leia diretamente `cmd/api/routes.go` (arquivo conciso de ~110 linhas, fonte viva da verdade).
- **Trabalho incremental:** Em alterações complexas, formule um plano antes de editar arquivos para evitar retrabalho e consumo desnecessário de contexto.

---

## Contexto Especializado Sob Demanda (Consulte apenas se a tarefa envolver o tema)

Não carregue estes arquivos no contexto se a sua tarefa não estiver diretamente relacionada a eles:

- **Migrations, Schema do Postgres e Testcontainers:** Leia `documentation/database.md`.
- **Planos, Quotas, Limites e Mock de Cobrança:** Leia `documentation/billing.md`.
- **Classificador Naive Bayes, Merchant Rules ou OFX:** Leia `documentation/ai_classifier.md`.
- **Diretrizes de Produção, Negócios e Infraestrutura:** Leia `documentation/production_launch_and_billing_plan.md`.
