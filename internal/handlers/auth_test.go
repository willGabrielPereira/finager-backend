package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

// TestLoginUpdatesLastLoginAt confirma que um login bem-sucedido via
// POST /auth/login atualiza last_login_at do usuário no banco.
func TestLoginUpdatesLastLoginAt(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)
	require.NoError(t, repos.EnsureIndexes(context.Background()))

	authSvc := auth.NewService("teste-secret-login", 1)
	authHandler := auth.NewHandler(authSvc, repos.Users, repos.Families, repos.RefreshTokens, repos.Blocklist, repos.Invites, repos.SignupInvites, 1, middleware.NewInMemoryRateLimiter(10, time.Minute))

	mux := http.NewServeMux()
	mux.Handle("POST /auth/login", http.HandlerFunc(authHandler.Login))

	ctx := context.Background()

	fam := &models.Family{Name: "Família Login " + uuid.NewString()}
	require.NoError(t, repos.Families.Create(ctx, fam))

	password := "SenhaForte123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4) // custo baixo para teste rápido
	require.NoError(t, err)

	login := "login_" + uuid.NewString()[:8]
	user := &models.User{
		Login:        login,
		Email:        uuid.NewString() + "@example.com",
		PasswordHash: string(hash),
		FamilyID:     fam.ID,
	}
	require.NoError(t, repos.Users.Create(ctx, user))
	require.NoError(t, repos.Families.AddMember(ctx, fam.ID, user.ID))

	// Antes do login, last_login_at deve estar NULL.
	before, err := repos.Users.FindByID(ctx, user.ID)
	require.NoError(t, err)
	assert.Nil(t, before.LastLoginAt)

	payload, err := json.Marshal(map[string]string{"login": login, "password": password})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	after, err := repos.Users.FindByID(ctx, user.ID)
	require.NoError(t, err)
	require.NotNil(t, after.LastLoginAt)
	assert.WithinDuration(t, time.Now(), *after.LastLoginAt, 5*time.Second)
}
