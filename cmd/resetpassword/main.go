// cmd/resetpassword/main.go — Reseta manualmente a senha de um usuário (uso: make reset-password ID=<login_ou_email> PASSWORD=<nova_senha_opcional>)
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/database"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

func main() {
	_ = godotenv.Load()

	// Validar argumentos
	if len(os.Args) < 2 {
		fmt.Println("Uso: make reset-password ID=<login_ou_email> PASSWORD=<nova_senha_opcional>")
		fmt.Println("\nExemplo:")
		fmt.Println("  make reset-password ID=john.doe@example.com")
		fmt.Println("  make reset-password ID=john PASSWORD='MinhaNovaSenh@123'")
		os.Exit(1)
	}

	identifier := os.Args[1]
	var newPassword string

	// Se o segundo argumento foi fornecido, use-o; caso contrário, peça interativamente
	if len(os.Args) > 2 && os.Args[2] != "" {
		newPassword = os.Args[2]
	} else {
		fmt.Print("Nova senha: ")
		fmt.Scanln(&newPassword)
	}

	// Validar a nova senha: 8-72 caracteres (mesmo padrão de registerRequest)
	if len(newPassword) < 8 || len(newPassword) > 72 {
		log.Fatalf("Erro: a senha deve ter entre 8 e 72 caracteres (atual: %d)", len(newPassword))
	}

	dbDsn := getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/finager?sslmode=disable")

	db, err := database.Connect(dbDsn)
	if err != nil {
		log.Fatalf("Falha ao conectar ao PostgreSQL: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repos := repository.New(db.Pool)

	// Buscar o usuário
	user, err := repos.Users.FindByLoginOrEmail(ctx, identifier)
	if err != nil {
		log.Fatalf("Usuário não encontrado: %v", err)
	}

	// Mostrar dados do usuário e pedir confirmação
	fmt.Printf("\nIsto vai resetar a senha do usuário %q (email: %s, id: %s).\n", user.Login, user.Email, user.ID.String())
	fmt.Print("   Digite 'sim' para confirmar: ")

	var confirm string
	fmt.Scanln(&confirm)
	if strings.TrimSpace(strings.ToLower(confirm)) != "sim" {
		fmt.Println("Operação cancelada.")
		os.Exit(0)
	}

	// Gerar hash da nova senha com bcrypt (cost 12, igual ao usado em handler.go)
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		log.Fatalf("Erro ao gerar hash de senha: %v", err)
	}

	// Atualizar a senha do usuário
	if err := repos.Users.UpdatePassword(ctx, user.ID, string(hashed)); err != nil {
		log.Fatalf("Erro ao atualizar senha: %v", err)
	}

	// Revogar todos os refresh tokens (derrubar todas as sessões ativas)
	if err := repos.RefreshTokens.RevokeAllByUser(ctx, user.ID); err != nil {
		log.Fatalf("Erro ao revogar sessões: %v", err)
	}

	// Mensagem de sucesso
	fmt.Printf("\n✓ Senha resetada com sucesso para %q. Todas as sessões ativas foram revogadas.\n", user.Login)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
