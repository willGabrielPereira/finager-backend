package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/handlers"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

func TestDeleteAccountSecurityAndCascade(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-me", 1)
	authMid := middleware.Authenticate(authSvc, repos.Blocklist)

	profileHandler := handlers.NewProfileHandler(repos.Users, repos.Families, nil)
	mux := http.NewServeMux()
	mux.Handle("DELETE /me", authMid(http.HandlerFunc(profileHandler.Delete)))

	// Helper para hashear senha
	hashPassword := func(pw string) string {
		h, _ := bcrypt.GenerateFromPassword([]byte(pw), 4) // custo baixo para teste rápido
		return string(h)
	}

	ctx := context.Background()

	t.Run("Rejeita exclusão quando senha não é fornecida ou está vazia", func(t *testing.T) {
		fam := &models.Family{Name: "Família Solitária"}
		require.NoError(t, repos.Families.Create(ctx, fam))

		user := &models.User{
			Login:        "user_empty_pw",
			PasswordHash: hashPassword("senha123"),
			FamilyID:     fam.ID,
		}
		require.NoError(t, repos.Users.Create(ctx, user))
		require.NoError(t, repos.Families.AddMember(ctx, fam.ID, user.ID))

		token, err := authSvc.GenerateToken(user)
		require.NoError(t, err)

		// 1. Sem body
		req := httptest.NewRequest("DELETE", "/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// 2. Com senha vazia
		payload, _ := json.Marshal(map[string]string{"password": "   "})
		req = httptest.NewRequest("DELETE", "/me", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("Rejeita exclusão com senha incorreta", func(t *testing.T) {
		fam := &models.Family{Name: "Família Protegida"}
		require.NoError(t, repos.Families.Create(ctx, fam))

		user := &models.User{
			Login:        "user_wrong_pw",
			PasswordHash: hashPassword("segredo_correto"),
			FamilyID:     fam.ID,
		}
		require.NoError(t, repos.Users.Create(ctx, user))
		require.NoError(t, repos.Families.AddMember(ctx, fam.ID, user.ID))

		token, err := authSvc.GenerateToken(user)
		require.NoError(t, err)

		payload, _ := json.Marshal(map[string]string{"password": "senha_errada"})
		req := httptest.NewRequest("DELETE", "/me", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		// Verifica que o usuário e a família continuam intactos no banco
		u, err := repos.Users.FindByID(ctx, user.ID)
		assert.NoError(t, err)
		assert.NotNil(t, u)
	})

	t.Run("Exclui com sucesso usuário único e cascata a família inteira", func(t *testing.T) {
		fam := &models.Family{Name: "Família Individual"}
		require.NoError(t, repos.Families.Create(ctx, fam))

		user := &models.User{
			Login:        "user_sole",
			PasswordHash: hashPassword("minha_senha_123"),
			FamilyID:     fam.ID,
		}
		require.NoError(t, repos.Users.Create(ctx, user))
		require.NoError(t, repos.Families.AddMember(ctx, fam.ID, user.ID))

		// Cria uma conta bancária para este usuário na família
		acc := &models.Account{
			Name:         "Conta Nubank",
			Institution:  "Nubank",
			FamilyID:     fam.ID,
			CreatedBy:    user.ID,
			AllowedUsers: []uuid.UUID{},
		}
		require.NoError(t, repos.Accounts.Create(ctx, acc))

		token, err := authSvc.GenerateToken(user)
		require.NoError(t, err)

		payload, _ := json.Marshal(map[string]string{"password": "minha_senha_123"})
		req := httptest.NewRequest("DELETE", "/me", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// 1. O usuário deve ter sido excluído
		deletedUser, err := repos.Users.FindByID(ctx, user.ID)
		assert.Error(t, err)
		assert.Nil(t, deletedUser)

		// 2. A família DEVE ter sido excluída porque era a única conta vinculada
		deletedFam, err := repos.Families.FindByID(ctx, fam.ID)
		assert.Error(t, err)
		assert.Nil(t, deletedFam)

		// 3. As contas da família devem ter sido excluídas
		deletedAcc, err := repos.Accounts.FindByID(ctx, acc.ID)
		assert.Error(t, err)
		assert.Nil(t, deletedAcc)
	})

	t.Run("Exclui usuário mas preserva família quando há outros membros", func(t *testing.T) {
		fam := &models.Family{Name: "Família Conjunta"}
		require.NoError(t, repos.Families.Create(ctx, fam))

		userAlice := &models.User{
			Login:        "alice_leaving",
			PasswordHash: hashPassword("senha_alice"),
			FamilyID:     fam.ID,
		}
		userBob := &models.User{
			Login:        "bob_staying",
			PasswordHash: hashPassword("senha_bob"),
			FamilyID:     fam.ID,
		}
		require.NoError(t, repos.Users.Create(ctx, userAlice))
		require.NoError(t, repos.Users.Create(ctx, userBob))
		require.NoError(t, repos.Families.AddMember(ctx, fam.ID, userAlice.ID))
		require.NoError(t, repos.Families.AddMember(ctx, fam.ID, userBob.ID))

		// Alice criou uma conta na família
		acc := &models.Account{
			Name:         "Conta Conjunta Itaú",
			Institution:  "Itaú",
			FamilyID:     fam.ID,
			CreatedBy:    userAlice.ID,
			AllowedUsers: []uuid.UUID{},
		}
		require.NoError(t, repos.Accounts.Create(ctx, acc))

		tokenAlice, err := authSvc.GenerateToken(userAlice)
		require.NoError(t, err)

		// Alice exclui sua conta informando a senha
		payload, _ := json.Marshal(map[string]string{"password": "senha_alice"})
		req := httptest.NewRequest("DELETE", "/me", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+tokenAlice)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// 1. Alice foi excluída
		deletedAlice, err := repos.Users.FindByID(ctx, userAlice.ID)
		assert.Error(t, err)
		assert.Nil(t, deletedAlice)

		// 2. Bob continua existindo
		survivingBob, err := repos.Users.FindByID(ctx, userBob.ID)
		assert.NoError(t, err)
		assert.NotNil(t, survivingBob)

		// 3. A família CONTINUA existindo para o Bob
		survivingFam, err := repos.Families.FindByID(ctx, fam.ID)
		assert.NoError(t, err)
		assert.NotNil(t, survivingFam)

		// 4. A conta bancária teve a autoria transferida para o Bob
		updatedAcc, err := repos.Accounts.FindByID(ctx, acc.ID)
		assert.NoError(t, err)
		assert.NotNil(t, updatedAcc)
		assert.Equal(t, userBob.ID, updatedAcc.CreatedBy)
	})
}
