.PHONY: docs build

## docs: Regenera a documentação Swagger a partir das anotações nos handlers
docs:
	swag init -g cmd/api/main.go --output docs

## build: Compila o binário localmente
build: docs
	go build -o finager ./cmd/api

## run: Roda a API localmente (requer MongoDB na porta 27017)
run: docs
	go run ./cmd/api

## up: Sobe o ambiente completo via Docker Compose
up:
	docker compose up --build
