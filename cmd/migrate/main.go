// cmd/migrate — Executa as migrations de banco de dados do Finager.
//
// Uso:
//
//	go run ./cmd/migrate            # equivalente a "up"
//	go run ./cmd/migrate up         # aplica todas as migrations pendentes
//	go run ./cmd/migrate status     # exibe o status de cada migration
//
// Variáveis de ambiente (mesmas do servidor principal):
//
//	MONGO_URI   (default: mongodb://localhost:27017)
//	MONGO_DB    (default: finager)
//
// No Docker, execute antes de reiniciar o contêiner da API:
//
//	docker run --rm --env-file .env finager-migrate up
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/willGabrielPereira/finager-backend/internal/database"
	"github.com/willGabrielPereira/finager-backend/internal/migrations"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Sem .env, usando variáveis de ambiente do sistema")
	}

	mongoURI := getEnv("MONGO_URI", "mongodb://localhost:27017")
	mongoDB := getEnv("MONGO_DB", "finager")

	db, err := database.Connect(mongoURI, mongoDB)
	if err != nil {
		log.Fatalf("Falha ao conectar ao MongoDB: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	runner := migrations.NewRunner(db.DB, migrations.All())

	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "up":
		log.Println("Executando migrations pendentes...")
		if err := runner.Up(ctx); err != nil {
			log.Fatalf("ERRO: %v", err)
		}
	case "status":
		if err := runner.Status(ctx); err != nil {
			log.Fatalf("ERRO: %v", err)
		}
	default:
		log.Printf("Comando desconhecido: %q", command)
		log.Println("Comandos disponíveis: up, status")
		os.Exit(1)
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
