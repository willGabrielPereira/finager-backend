// cmd/seed/main.go — Popula o banco com 2 usuários e 1 família compartilhada.
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
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/database"
	"github.com/willGabrielPereira/finager-backend/internal/models"
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

	// Ensure indexes before inserting.
	if err := repos.EnsureIndexes(ctx); err != nil {
		log.Fatalf("EnsureIndexes: %v", err)
	}

	// ── Create / find the shared family ──────────────────────────────────────
	family := ensureFamily(ctx, db.DB, familyName)
	log.Printf("✓ Family  : %q (id: %s)", family.Name, family.ID.Hex())

	// ── Create / find users ───────────────────────────────────────────────────
	user1 := ensureUser(ctx, repos.Users, user1Login, user1Password, family.ID)
	log.Printf("✓ User 1  : %q (id: %s)", user1.Login, user1.ID.Hex())

	user2 := ensureUser(ctx, repos.Users, user2Login, user2Password, family.ID)
	log.Printf("✓ User 2  : %q (id: %s)", user2.Login, user2.ID.Hex())

	// ── Link both users to the family ─────────────────────────────────────────
	if err := repos.Families.AddMember(ctx, family.ID, user1.ID); err != nil {
		log.Fatalf("AddMember user1: %v", err)
	}
	if err := repos.Families.AddMember(ctx, family.ID, user2.ID); err != nil {
		log.Fatalf("AddMember user2: %v", err)
	}

	// ── Create Default System Tags ────────────────────────────────────────────
	ensureSystemTags(ctx, repos.Tags)
	log.Println("✓ System Tags : ensured")

	// ── Create Initial Bank Accounts ──────────────────────────────────────────
	// Uma conta Compartilhada sem array de permissões restritas (Todos enxergam)
	sharedAcc := ensureAccount(ctx, repos.Accounts, "Conta Conjunta", "Nubank", family.ID, user1.ID, nil)
	log.Printf("✓ Shared Acc: %q (id: %s)", sharedAcc.Name, sharedAcc.ID.Hex())

	// Uma conta Privada (Private list só enxerga user1)
	privAcc := ensureAccount(ctx, repos.Accounts, "Conta Privada "+user1.Login, "Itaú", family.ID, user1.ID, []bson.ObjectID{user1.ID})
	log.Printf("✓ Private Acc: %q (id: %s)", privAcc.Name, privAcc.ID.Hex())

	log.Println("✓ Seed completed successfully.")
}

// ensureFamily finds a family by name or creates it if it does not exist.
func ensureFamily(ctx context.Context, db *mongo.Database, name string) *models.Family {
	col := db.Collection("families")
	var family models.Family
	err := col.FindOne(ctx, bson.D{{Key: "name", Value: name}}).Decode(&family)
	if err == nil {
		return &family
	}

	family = models.Family{
		ID:        bson.NewObjectID(),
		Name:      name,
		MemberIDs: []bson.ObjectID{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if _, err := col.InsertOne(ctx, family); err != nil {
		log.Fatalf("InsertOne family: %v", err)
	}
	return &family
}

// ensureUser finds a user by login or creates them with the given password.
// If the user already exists, their family_id is updated to the given family.
func ensureUser(ctx context.Context, repo *repository.UserRepository, login, password string, familyID bson.ObjectID) *models.User {
	user, err := repo.FindByLogin(ctx, login)
	if err == nil {
		// User exists — ensure they're in the right family.
		if err := repo.UpdateFamilyID(ctx, user.ID, familyID); err != nil {
			log.Fatalf("UpdateFamilyID(%s): %v", login, err)
		}
		user.FamilyID = familyID
		return user
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Fatalf("bcrypt(%s): %v", login, err)
	}

	newUser := &models.User{
		Login:        login,
		PasswordHash: string(hash),
		FamilyID:     familyID,
	}
	if err := repo.Create(ctx, newUser); err != nil {
		log.Fatalf("Create user(%s): %v", login, err)
	}
	return newUser
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("Required environment variable %q is not set", key)
	}
	return v
}

func ensureSystemTags(ctx context.Context, repo *repository.TagRepository) {
	// Padrões hardcoded para inicializar a esteira limpa da família
	defs := []models.Tag{
		{Name: "Alimentação", Color: "#E53935", Icon: "restaurant", IsSystem: true},
		{Name: "Educação", Color: "#1E88E5", Icon: "school", IsSystem: true},
		{Name: "Transporte", Color: "#FDD835", Icon: "directions_car", IsSystem: true},
		{Name: "Saúde", Color: "#43A047", Icon: "local_hospital", IsSystem: true},
		{Name: "Moradia", Color: "#8E24AA", Icon: "home", IsSystem: true},
		{Name: "Salário", Color: "#00897B", Icon: "attach_money", IsSystem: true},
		{Name: "Streamings", Color: "#FF0000", Icon: "subscriptions", IsSystem: true},
	}

	// Como agora o repositório possuí a função de Upsert Idempotente para a Tag do Sistema,
	// ele não criará IDs repetidos. Se a cor ou ícone mudarem aqui no deploy, ele automaticamente atualiza o banco em produção!
	for _, rawtag := range defs {
		if err := repo.UpsertSystemTag(ctx, &rawtag); err != nil {
			log.Printf("Aviso: Falha ao rodar Upsert na Tag %s: %v", rawtag.Name, err)
		}
	}
}

func ensureAccount(ctx context.Context, repo *repository.AccountRepository, name, institution string, familyID, creatorID bson.ObjectID, allowedUsers []bson.ObjectID) *models.Account {
	// Simula Idempotência pela instituição e nome na familia
	visible, _ := repo.FindVisibleAccounts(ctx, familyID, creatorID)
	for _, acc := range visible {
		if acc.Name == name && acc.Institution == institution {
			return acc
		}
	}

	acc := &models.Account{
		Name:         name,
		Institution:  institution,
		FamilyID:     familyID,
		CreatedBy:    creatorID,
		AllowedUsers: allowedUsers,
	}

	if err := repo.Create(ctx, acc); err != nil {
		log.Fatalf("Failed to create account %q: %v", name, err)
	}

	return acc
}
