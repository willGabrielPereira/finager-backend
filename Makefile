.PHONY: docs build seed migrate migrate-status

## docs: Regenera a documentação Swagger a partir das anotações nos handlers
docs:
	swag init -g cmd/api/main.go --output docs

## build: Compila o binário localmente
build: docs
	go build -o finager ./cmd/api

## run: Roda a API localmente (requer MongoDB na porta 27017)
run: docs
	go run ./cmd/api

## seed: Cria os usuários e família iniciais no MongoDB (idempotente)
seed:
	go run ./cmd/seed

## migrate: Aplica todas as migrations de banco de dados pendentes
migrate:
	go run ./cmd/migrate up

## migrate-status: Exibe o status de cada migration (APPLIED / PENDING)
migrate-status:
	go run ./cmd/migrate status

## up: Sobe o ambiente completo via Docker Compose
up:
	docker compose up --build
