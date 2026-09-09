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
	"github.com/willGabrielPereira/finager-backend/internal/database"
)

func main() {
	_ = godotenv.Load()

	dbDsn := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/finager?sslmode=disable")

	// Confirmação de segurança via stdin
	fmt.Println("⚠️  Isso irá apagar TODAS as transações do banco de dados.")
	fmt.Print("   Digite 'sim' para confirmar: ")

	var confirm string
	fmt.Scanln(&confirm)
	if strings.TrimSpace(strings.ToLower(confirm)) != "sim" {
		fmt.Println("Operação cancelada.")
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.Connect(dbDsn)
	if err != nil {
		log.Fatalf("Falha ao conectar ao PostgreSQL: %v", err)
	}
	defer db.Close()

	tables := []string{"transaction_tags", "transactions", "classifier_states"}
	for _, table := range tables {
		cmdTag, err := db.Pool.Exec(ctx, "DELETE FROM " + table)
		if err != nil {
			log.Fatalf("Erro ao limpar tabela %q: %v", table, err)
		}
		fmt.Printf("✓ %-22s %d linha(s) removida(s)\n", table+":", cmdTag.RowsAffected())
	}

	fmt.Println("\n✓ Reset concluído. Suba o OFX novamente para reimportar as transações.")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
