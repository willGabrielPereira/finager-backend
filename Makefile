.PHONY: docs build run seed seed-tags migrate migrate-status up down clean fresh reset-transactions

## docs: Regenera a documentação Swagger a partir das anotações nos handlers
docs:
	swag init -g cmd/api/main.go --output docs

## build: Compila o binário localmente
build: docs
	go build -o finager ./cmd/api

## run: Roda a API localmente (requer PostgreSQL na porta 5432)
run: docs
	go run ./cmd/api

## seed: Cria os usuários e família iniciais (idempotente)
seed:
	go run ./cmd/seed

## seed-tags: Popula apenas as tags de sistema no banco (clean/cold start, sem usuários nem contas fakes)
seed-tags:
	go run ./cmd/seed --tags-only

## migrate: Aplica todas as migrations de banco de dados pendentes
migrate:
	go run ./cmd/migrate up

## migrate-status: Exibe o status de cada migration (APPLIED / PENDING)
migrate-status:
	go run ./cmd/migrate status

## up: Sobe o ambiente completo via Docker Compose
up:
	docker compose up --build

## down: Para os containers do Docker Compose
down:
	docker compose down

## clean: Para containers e apaga volumes do banco (clean start absoluto)
clean:
	docker compose down -v

## fresh: Limpa os volumes e sobe tudo reconstruído do zero
fresh: clean up

## reset-transactions: Apaga TODAS as transações e o estado do classificador de IA (preserva usuários, família, contas e tags)
reset-transactions:
	go run ./cmd/reset
