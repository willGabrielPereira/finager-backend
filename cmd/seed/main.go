// cmd/seed/main.go — Popula o banco com usuários, família, tags e contas.
//
// Uso: make seed
//
// Variáveis de ambiente necessárias (além das do .env principal):
//
//	USER1_LOGIN, USER1_PASSWORD
//	USER2_LOGIN, USER2_PASSWORD
//	SEED_FAMILY_NAME  (opcional, default: "Família Principal")
//
// O script é idempotente: pode ser executado múltiplas vezes sem criar duplicatas.
package main

import (
	"context"
	"log"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/database"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file, reading from environment variables")
	}

	mongoURI := getEnv("MONGO_URI", "mongodb://localhost:27017")
	mongoDB := getEnv("MONGO_DB", "finager")

	user1Login := requireEnv("USER1_LOGIN")
	user1Password := requireEnv("USER1_PASSWORD")
	user2Login := requireEnv("USER2_LOGIN")
	user2Password := requireEnv("USER2_PASSWORD")
	familyName := getEnv("SEED_FAMILY_NAME", "Família Principal")

	// ── Database ──────────────────────────────────────────────────────────────
	db, err := database.Connect(mongoURI, mongoDB)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repos := repository.New(db.DB)

	if err := repos.EnsureIndexes(ctx); err != nil {
		log.Fatalf("EnsureIndexes: %v", err)
	}

	// ── Família ───────────────────────────────────────────────────────────────
	family := ensureFamily(ctx, db.DB, familyName)
	log.Printf("✓ Family      : %q (id: %s)", family.Name, family.ID.Hex())

	// ── Usuários ──────────────────────────────────────────────────────────────
	user1 := ensureUser(ctx, repos.Users, user1Login, user1Password, family.ID)
	log.Printf("✓ User 1      : %q (id: %s)", user1.Login, user1.ID.Hex())

	user2 := ensureUser(ctx, repos.Users, user2Login, user2Password, family.ID)
	log.Printf("✓ User 2      : %q (id: %s)", user2.Login, user2.ID.Hex())

	if err := repos.Families.AddMember(ctx, family.ID, user1.ID); err != nil {
		log.Fatalf("AddMember user1: %v", err)
	}
	if err := repos.Families.AddMember(ctx, family.ID, user2.ID); err != nil {
		log.Fatalf("AddMember user2: %v", err)
	}

	// ── Tags de sistema ───────────────────────────────────────────────────────
	// ensureSystemTags retorna o mapa nome→ID para as tag rules usarem
	nameToID := ensureSystemTags(ctx, repos.Tags)
	log.Printf("✓ System Tags : %d tags ensured", len(nameToID))

	// ── Regras de auto-tagging ────────────────────────────────────────────────
	ensureSystemTagRules(ctx, repos.TagRules, nameToID)
	log.Printf("✓ Tag Rules   : %d rules ensured", len(systemTagRuleDefs))

	// ── Contas bancárias ──────────────────────────────────────────────────────
	sharedAcc := ensureAccount(ctx, repos.Accounts, "Conta Conjunta", "Nubank", family.ID, user1.ID, nil)
	log.Printf("✓ Shared Acc  : %q (id: %s)", sharedAcc.Name, sharedAcc.ID.Hex())

	privAcc := ensureAccount(ctx, repos.Accounts, "Conta Privada "+user1.Login, "Itaú", family.ID, user1.ID, []bson.ObjectID{user1.ID})
	log.Printf("✓ Private Acc : %q (id: %s)", privAcc.Name, privAcc.ID.Hex())

	log.Println("✓ Seed completed successfully.")
}
