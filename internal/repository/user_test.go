package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

func TestUserRepository(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()

	// Inicia o container de repositórios (já que o repo do usuário fica dentro dele agora)
	repos := repository.New(db)
	err := repos.EnsureIndexes(ctx)
	require.NoError(t, err, "falha ao garantir índices")

	t.Run("Create and FindByLogin", func(t *testing.T) {
		family := &models.Family{ID: uuid.New(), Name: "Test Family"}
		_ = repos.Families.Create(ctx, family)
		familyID := family.ID
		pwHash, _ := bcrypt.GenerateFromPassword([]byte("123456"), 4)

		user := &models.User{
			Login:        "teste1",
			PasswordHash: string(pwHash),
			FamilyID:     familyID,
		}

		err = repos.Users.Create(ctx, user)
		require.NoError(t, err)

		// Verifica se foi gerado o ObjectId e timestamps
		assert.NotEqual(t, uuid.Nil, user.ID)
		assert.False(t, user.CreatedAt.IsZero())

		// Busca e tenta dar o match
		found, err := repos.Users.FindByLogin(ctx, "teste1")
		require.NoError(t, err)
		assert.Equal(t, user.ID, found.ID)
		assert.Equal(t, "teste1", found.Login)
		assert.Equal(t, familyID, found.FamilyID)
	})

	t.Run("Unique Login Index", func(t *testing.T) {
		family := &models.Family{ID: uuid.New(), Name: "Test Family 2"}
		_ = repos.Families.Create(ctx, family)
		familyID := family.ID
		
		u1 := &models.User{Login: "duplicated", FamilyID: familyID}
		u2 := &models.User{Login: "duplicated", FamilyID: familyID}

		err := repos.Users.Create(ctx, u1)
		require.NoError(t, err)

		err = repos.Users.Create(ctx, u2)
		// Deve dar um erro do mongo sobre duplicação de chave
		require.Error(t, err)
	})

	t.Run("UpdatePassword", func(t *testing.T) {
		family := &models.Family{ID: uuid.New(), Name: "Test Family 3"}
		_ = repos.Families.Create(ctx, family)
		u := &models.User{Login: "changepw", FamilyID: family.ID}
		_ = repos.Users.Create(ctx, u)

		newHash := "new-fake-hash"
		time.Sleep(2 * time.Millisecond) // Garante precisão pra não competir com truncamento de ms do BSON
		err := repos.Users.UpdatePassword(ctx, u.ID, newHash)
		require.NoError(t, err)

		found, _ := repos.Users.FindByID(ctx, u.ID)
		assert.Equal(t, newHash, found.PasswordHash)
	})
}
