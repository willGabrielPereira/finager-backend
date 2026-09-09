// cmd/reset/main.go — Apaga todas as transações e o estado do classificador de IA.
//
// Uso: make reset-transactions
//
// Preserva: usuários, família, contas bancárias e tags.
// Remove:   collections `transactions` e `classifier_states`.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
	_ = godotenv.Load()

	mongoURI := getEnv("MONGO_URI", "mongodb://localhost:27017")
	mongoDB := getEnv("MONGO_DB", "finager")

	// Confirmação de segurança via stdin
	fmt.Printf("⚠️  Isso irá apagar TODAS as transações do banco \"%s\".\n", mongoDB)
	fmt.Print("   Digite 'sim' para confirmar: ")

	var confirm string
	fmt.Scanln(&confirm)
	if strings.TrimSpace(strings.ToLower(confirm)) != "sim" {
		fmt.Println("Operação cancelada.")
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Falha ao conectar ao MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database(mongoDB)

	collections := []string{"transactions", "classifier_states"}
	for _, col := range collections {
		res, err := db.Collection(col).DeleteMany(ctx, bson.M{})
		if err != nil {
			log.Fatalf("Erro ao limpar collection %q: %v", col, err)
		}
		fmt.Printf("✓ %-22s %d doc(s) removido(s)\n", col+":", res.DeletedCount)
	}

	fmt.Println("\n✓ Reset concluído. Suba o OFX novamente para reimportar as transações.")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
