# Finager API

API REST em Go para gestão financeira pessoal. Importa extratos bancários no formato OFX, persiste as transações no MongoDB e permite filtragem e tagueamento para relatórios de gastos.

---

## Stack

| Camada | Tecnologia |
|---|---|
| Linguagem | Go 1.25 |
| Banco de dados | MongoDB 7 |
| Driver MongoDB | `go.mongodb.org/mongo-driver/v2` |
| Parser OFX | `github.com/aclindsa/ofxgo` |
| Autenticação | JWT via `github.com/golang-jwt/jwt/v5` |
| Env vars | `github.com/joho/godotenv` |
| Documentação | Swagger UI via `github.com/swaggo/swag` |
| Infraestrutura | Docker + Docker Compose |

---

## Estrutura de diretórios

```
finager/
├── cmd/
│   └── api/
│       └── main.go              # Entrypoint: HTTP server + graceful shutdown
├── docs/                        # Gerado automaticamente pelo swag init
├── internal/
│   ├── auth/
│   │   ├── handler.go           # POST /auth/login
│   │   └── jwt.go               # Geração e validação de tokens JWT
│   ├── config/
│   │   └── config.go            # Leitura e validação de variáveis de ambiente
│   ├── database/
│   │   └── mongo.go             # Conexão com o MongoDB
│   ├── handlers/
│   │   ├── health.go            # GET /health
│   │   ├── me.go                # GET /me
│   │   └── transaction.go       # GET /transactions, POST /transactions/import
│   ├── middleware/
│   │   └── auth.go              # Middleware de autenticação JWT
│   ├── models/
│   │   └── transaction.go       # Model Transaction
│   ├── ofxparser/
│   │   └── parser.go            # Leitura e conversão de arquivos OFX
│   └── repository/
│       └── transaction.go       # Operações no MongoDB (BulkUpsert, List)
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
| `MONGO_URI` | URI de conexão do MongoDB | Não (default: `mongodb://localhost:27017`) |
| `MONGO_DB` | Nome do banco de dados | Não (default: `finager`) |
| `PORT` | Porta HTTP da API | Não (default: `8080`) |
| `API_LOGIN` | Login do usuário administrador | Não (default: `admin`) |
| `API_PASSWORD` | Senha do usuário administrador | **Sim** |
| `JWT_SECRET` | Chave secreta para assinar os tokens JWT | **Sim** |
| `JWT_EXPIRATION_HOURS` | Tempo de expiração do token em horas | Não (default: `24`) |

Para gerar um `JWT_SECRET` seguro:
```bash
openssl rand -hex 32
```

---

## Rodando localmente

### Com Docker Compose (recomendado)

Sobe a API e o MongoDB juntos:

```bash
docker compose up --build
```

### Sem Docker

Requer MongoDB rodando localmente na porta `27017`.

```bash
make run
# ou
go run ./cmd/api
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

| Método | Rota | Auth | Descrição |
|---|---|---|---|
| `GET` | `/health` | ❌ | Status da API |
| `POST` | `/auth/login` | ❌ | Autentica e retorna um JWT |
| `GET` | `/me` | ✅ | Retorna o usuário autenticado |
| `POST` | `/transactions/import` | ✅ | Importa um arquivo OFX |
| `GET` | `/transactions` | ✅ | Lista transações com filtros e paginação |

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
make docs    # Regenera a documentação Swagger
make build   # Compila o binário
make run     # Roda a API localmente
make up      # docker compose up --build
```

> **Atenção:** Sempre rode `make docs` (ou `swag init -g cmd/api/main.go --output docs`) após alterar anotações nos handlers. O `docker compose up --build` já faz isso automaticamente.
