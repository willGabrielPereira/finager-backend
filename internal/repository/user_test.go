package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

func TestUserRepository(t *testing.T) {
	// Prepara um Mongo DB descartável, zerado só pra esse teste
	db := testutil.SetupMongoDB(t)
	ctx := context.Background()

	// Inicia o container de repositórios (já que o repo do usuário fica dentro dele agora)
	repos := repository.New(db)
	err := repos.EnsureIndexes(ctx)
	require.NoError(t, err, "falha ao garantir índices")

	t.Run("Create and FindByLogin", func(t *testing.T) {
		familyID := bson.NewObjectID()
		pwHash, _ := bcrypt.GenerateFromPassword([]byte("123456"), 4)

		user := &models.User{
			Login:        "teste1",
			PasswordHash: string(pwHash),
			FamilyID:     familyID,
		}

		err = repos.Users.Create(ctx, user)
		require.NoError(t, err)

		// Verifica se foi gerado o ObjectId e timestamps
		assert.False(t, user.ID.IsZero())
		assert.False(t, user.CreatedAt.IsZero())

		// Busca e tenta dar o match
		found, err := repos.Users.FindByLogin(ctx, "teste1")
		require.NoError(t, err)
		assert.Equal(t, user.ID, found.ID)
		assert.Equal(t, "teste1", found.Login)
		assert.Equal(t, familyID, found.FamilyID)
	})

	t.Run("Unique Login Index", func(t *testing.T) {
		familyID := bson.NewObjectID()
		
		u1 := &models.User{Login: "duplicated", FamilyID: familyID}
		u2 := &models.User{Login: "duplicated", FamilyID: familyID}

		err := repos.Users.Create(ctx, u1)
		require.NoError(t, err)

		err = repos.Users.Create(ctx, u2)
		// Deve dar um erro do mongo sobre duplicação de chave
		require.Error(t, err)
	})

	t.Run("UpdatePassword", func(t *testing.T) {
		u := &models.User{Login: "changepw", FamilyID: bson.NewObjectID()}
		_ = repos.Users.Create(ctx, u)

		newHash := "new-fake-hash"
		err := repos.Users.UpdatePassword(ctx, u.ID, newHash)
		require.NoError(t, err)

		found, _ := repos.Users.FindByID(ctx, u.ID)
		assert.Equal(t, newHash, found.PasswordHash)
		
		// Esperamos que o UpdatedAt seja mais atual (já que o Create gera com time.Now e Update também)
		assert.True(t, found.UpdatedAt.After(u.UpdatedAt) || found.UpdatedAt.Equal(u.UpdatedAt))
	})
}
