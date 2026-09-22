# Finager API

API REST em Go para gestão financeira familiar. Importa extratos bancários no formato OFX, persiste as transações no PostgreSQL e permite filtragem e tagueamento (manual ou automático via Naive Bayes) para relatórios de gastos.

---

## Stack

| Camada | Tecnologia |
|---|---|
| Linguagem | Go 1.25 |
| Banco de dados | PostgreSQL 15 |
| Driver PostgreSQL | `github.com/jackc/pgx/v5` (`pgxpool`) |
| Parser OFX | `github.com/aclindsa/ofxgo` |
| Autenticação | JWT via `github.com/golang-jwt/jwt/v5` |
| Env vars | `github.com/joho/godotenv` |
| Documentação | Swagger UI via `github.com/swaggo/swag` |
| Testes | `testcontainers-go` + `stretchr/testify` |
| Infraestrutura | Docker + Docker Compose |

---

## Estrutura de diretórios

```
finager/
├── cmd/
│   ├── api/                     # Entrypoint HTTP: main.go + routes.go
│   ├── migrate/                 # Runner de migrations (up / status)
│   ├── seed/                    # Seed idempotente (usuários, família, tags, contas)
│   └── reset/                   # Limpa transações e estado do classificador
├── docs/                        # Gerado automaticamente pelo swag init (Swagger)
├── documentation/               # Documentação técnica, arquitetura e planos
├── internal/
│   ├── auth/                    # Login, registro, refresh, JWT
│   ├── billing/                 # Planos, cupons e provider de pagamento (mock)
│   ├── classifier/              # Naive Bayes para tagueamento automático
│   ├── config/                  # Leitura e validação de variáveis de ambiente
│   ├── database/                # Conexão pgxpool + schema.sql (usado nos testes)
│   ├── handlers/                # Handlers HTTP por recurso
│   ├── middleware/              # Auth, CORS, rate limit, security headers
│   ├── migrations/              # Migrations auto-registradas via init()
│   ├── models/                  # Structs de domínio
│   ├── ofxparser/               # Leitura e conversão de arquivos OFX
│   ├── repository/              # Acesso ao PostgreSQL (Container = DI)
│   ├── response/                # Helpers de resposta JSON
│   └── testutil/               # Postgres via Testcontainers + fixtures
├── pkg/validator/               # Validações compartilhadas
├── .env.example                 # Template de variáveis de ambiente
├── docker-compose.yml
├── Dockerfile
└── Makefile
```

---

## Configuração

Copie o arquivo de exemplo e preencha os valores:

```bash
cp .env.example .env
```

| Variável | Descrição | Obrigatório |
|---|---|---|
| `DATABASE_URL` | DSN de conexão do PostgreSQL | Não (default: `postgres://postgres:postgres@localhost:5432/finager?sslmode=disable`) |
| `PORT` | Porta HTTP da API | Não (default: `8080`) |
| `JWT_SECRET` | Chave secreta para assinar os tokens JWT | **Sim** |
| `JWT_EXPIRATION_HOURS` | Expiração do access token em horas | Não (default: `1`) |
| `JWT_REFRESH_EXPIRATION_HOURS` | Expiração do refresh token em horas | Não (default: `168`) |
| `CORS_ALLOWED_ORIGINS` | Origens permitidas, separadas por vírgula | Não (default: `*`) |
| `USER1_LOGIN` / `USER1_PASSWORD` | Credenciais do 1º usuário criado por `make seed` | Somente para o seed |
| `USER2_LOGIN` / `USER2_PASSWORD` | Credenciais do 2º usuário criado por `make seed` | Somente para o seed |
| `SEED_FAMILY_NAME` | Nome da família criada pelo seed | Não (default: `Família Principal`) |

Para gerar um `JWT_SECRET` seguro:
```bash
openssl rand -hex 32
```

---

## Rodando localmente

### Com Docker Compose (recomendado)

Sobe a API e o PostgreSQL juntos. O container da API roda as migrations e o seed
de tags de sistema antes de iniciar o servidor:

```bash
docker compose up --build   # ou: make up
```

### Sem Docker

Requer PostgreSQL rodando localmente na porta `5432` e o CLI `swag` instalado
(`go install github.com/swaggo/swag/cmd/swag@latest`).

```bash
make migrate   # aplica as migrations
make seed      # cria usuários, família e tags iniciais (opcional)
make run       # gera os docs e sobe a API
```

---

## Documentação interativa (Swagger UI)

Com a API no ar, acesse:

```
http://localhost:8080/swagger/
```

Para testar rotas protegidas:
1. Faça login em `POST /auth/login` e copie o token
2. Clique em **Authorize** (cadeado no topo)
3. Informe `Bearer <token>`

---

## Rotas disponíveis

A lista completa e sempre atualizada de rotas vive em `cmd/api/routes.go`. As principais:

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| `GET` | `/health` | ❌ | Status da API |
| `GET` | `/swagger/` | ❌ | Swagger UI |
| `POST` | `/auth/login` \| `/auth/register` \| `/auth/refresh` | ❌ | Autenticação e emissão de tokens |
| `POST` \| `PUT` | `/auth/logout` \| `/auth/password` | ✅ | Logout e troca de senha |
| `GET` \| `PUT` \| `PATCH` \| `DELETE` | `/me` \| `/me/onboarding` | ✅ | Perfil do usuário e LGPD |
| `GET` \| `POST` | `/billing/plan`, `/billing/coupons/apply`, `/billing/simulate-upgrade`, `/billing/simulate-downgrade` | ✅ | Planos e faturamento |
| `GET` \| `POST` \| `DELETE` | `/family/*` | parcial | Membros e convites de família |
| `GET` \| `POST` \| `PUT` \| `DELETE` | `/transactions`, `/transactions/{id}`, `/transactions/import` | ✅ | CRUD e importação OFX de transações |
| `POST` | `/transactions/{id}/apply-similar`, `/transactions/{id}/suggest-tags`, `/transactions/ai-auto-tag` | ✅ | Tagueamento automático (IA) |
| `GET` \| `POST` \| `PUT` \| `DELETE` | `/tags`, `/tags/frequent`, `/tags/{id}` | ✅ | CRUD de tags |
| `GET` \| `POST` \| `DELETE` | `/merchant-rules`, `/merchant-rules/{id}` | ✅ | Regras de memória de estabelecimentos (Layer 1 da IA) |
| `GET` \| `POST` \| `PUT` \| `DELETE` | `/accounts`, `/accounts/{id}` | ✅ | CRUD de contas bancárias |

### `POST /auth/login`

```json
// Body
{ "login": "admin", "password": "sua-senha" }

// Response 200
{ "token": "eyJ..." }
```

### `POST /transactions/import`

Envie um arquivo OFX via `multipart/form-data` com o campo `file`.

```bash
curl -X POST http://localhost:8080/transactions/import \
  -H "Authorization: Bearer eyJ..." \
  -F "file=@extrato.ofx"
```

```json
// Response 200
{ "inserted": 42, "skipped": 0, "message": "importação concluída" }
```

Transações duplicadas (mesmo `FITID` + `account_id`) são ignoradas automaticamente.

### `GET /transactions`

| Parâmetro | Tipo | Descrição |
|---|---|---|
| `page` | int | Página (default: `1`) |
| `limit` | int | Itens por página, máx 100 (default: `20`) |
| `type` | string | `DEBIT` ou `CREDIT` |
| `tag` | string | Filtrar por tag |
| `date_from` | string | Data inicial — `YYYY-MM-DD` ou RFC3339 |
| `date_to` | string | Data final inclusiva — `YYYY-MM-DD` ou RFC3339 |
| `amount_min` | float | Valor mínimo |
| `amount_max` | float | Valor máximo |

```bash
# Exemplos
GET /transactions?type=DEBIT&date_from=2024-01-01&date_to=2024-01-31
GET /transactions?tag=alimentação&page=2&limit=50
GET /transactions?amount_min=100&amount_max=500
```

```json
// Response 200
{
  "data": [ /* array de Transaction */ ],
  "total": 142,
  "page": 1,
  "limit": 20,
  "total_pages": 8
}
```

---

## Comandos úteis

```bash
make docs                # Regenera a documentação Swagger
make build                # docs + compila o binário
make run                  # docs + roda a API localmente
make migrate               # Aplica migrations pendentes
make migrate-status        # Mostra status (APPLIED/PENDING) de cada migration
make seed                 # Cria usuários, família, contas e tags iniciais (idempotente)
make seed-tags              # Popula apenas as tags de sistema (cold start)
make reset-transactions      # Apaga transações e estado do classificador de IA
make up / down / clean / fresh   # Docker Compose: subir / parar / limpar volumes / reset completo
go test ./...              # Roda toda a suíte (requer Docker para os testes de integração)
```

> **Atenção:** Sempre rode `make docs` (ou `swag init -g cmd/api/main.go --output docs`) após alterar anotações nos handlers. O `docker compose up --build` já faz isso automaticamente.
