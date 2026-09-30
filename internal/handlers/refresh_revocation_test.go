package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

type refreshEnv struct {
	srv     *httptest.Server
	repos   *repository.Container
	authSvc *auth.Service
	userID  uuid.UUID
}

// setupRefreshEnv sobe Postgres, cria um usuário e expõe /auth/refresh e
// /auth/logout num httptest.Server real.
func setupRefreshEnv(t *testing.T) *refreshEnv {
	t.Helper()
	db, cleanup := testutil.SetupPostgresContainer(t)
	t.Cleanup(cleanup)
	repos := repository.New(db)
	ctx := context.Background()

	authSvc := auth.NewService("teste-secret-refresh", 1)
	h := auth.NewHandler(authSvc, repos.Users, repos.Families, repos.RefreshTokens, repos.Blocklist, repos.Invites, repos.SignupInvites, 1, middleware.NewInMemoryRateLimiter(10, time.Minute))

	mux := http.NewServeMux()
	mux.Handle("POST /auth/refresh", http.HandlerFunc(h.Refresh))
	mux.Handle("POST /auth/logout", http.HandlerFunc(h.Logout))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	fam := &models.Family{Name: "Família Refresh " + uuid.NewString()}
	require.NoError(t, repos.Families.Create(ctx, fam))
	user := &models.User{
		Login:        "refresh_" + uuid.NewString()[:8],
		Email:        uuid.NewString() + "@example.com",
		PasswordHash: "x",
		FamilyID:     fam.ID,
	}
	require.NoError(t, repos.Users.Create(ctx, user))
	require.NoError(t, repos.Families.AddMember(ctx, fam.ID, user.ID))

	return &refreshEnv{srv: srv, repos: repos, authSvc: authSvc, userID: user.ID}
}

// newToken persiste um refresh token com a expiração dada e devolve o valor bruto.
func (e *refreshEnv) newToken(t *testing.T, expiresAt time.Time) string {
	t.Helper()
	raw, hash, err := e.authSvc.GenerateRefreshToken()
	require.NoError(t, err)
	require.NoError(t, e.repos.RefreshTokens.Create(context.Background(), &models.RefreshToken{
		UserID: e.userID, TokenHash: hash, ExpiresAt: expiresAt,
	}))
	return raw
}

func (e *refreshEnv) post(t *testing.T, path, refreshToken string) int {
	t.Helper()
	body, err := json.Marshal(map[string]string{"refresh_token": refreshToken})
	require.NoError(t, err)
	resp, err := http.Post(e.srv.URL+path, "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestRefreshRevokedTokenReturns401(t *testing.T) {
	e := setupRefreshEnv(t)
	raw := e.newToken(t, time.Now().Add(time.Hour))

	assert.Equal(t, http.StatusOK, e.post(t, "/auth/refresh", raw))
	// Token já rotacionado (revogado) não pode ser reutilizado.
	assert.Equal(t, http.StatusUnauthorized, e.post(t, "/auth/refresh", raw))

	raw2 := e.newToken(t, time.Now().Add(time.Hour))
	require.NoError(t, e.repos.RefreshTokens.RevokeAllByUser(context.Background(), e.userID))
	assert.Equal(t, http.StatusUnauthorized, e.post(t, "/auth/refresh", raw2))
}

func TestRefreshAfterLogoutReturns401(t *testing.T) {
	e := setupRefreshEnv(t)
	raw := e.newToken(t, time.Now().Add(time.Hour))

	assert.Equal(t, http.StatusNoContent, e.post(t, "/auth/logout", raw))
	assert.Equal(t, http.StatusUnauthorized, e.post(t, "/auth/refresh", raw))
}

func TestRefreshExpiredTokenReturns401(t *testing.T) {
	e := setupRefreshEnv(t)
	raw := e.newToken(t, time.Now().Add(-time.Minute))

	assert.Equal(t, http.StatusUnauthorized, e.post(t, "/auth/refresh", raw))
}

func TestLogoutIdempotent(t *testing.T) {
	e := setupRefreshEnv(t)
	raw := e.newToken(t, time.Now().Add(time.Hour))
	expired := e.newToken(t, time.Now().Add(-time.Minute))

	assert.Equal(t, http.StatusNoContent, e.post(t, "/auth/logout", raw))
	assert.Equal(t, http.StatusNoContent, e.post(t, "/auth/logout", raw), "token já revogado")
	assert.Equal(t, http.StatusNoContent, e.post(t, "/auth/logout", expired), "token expirado")
	assert.Equal(t, http.StatusNoContent, e.post(t, "/auth/logout", "inexistente-"+uuid.NewString()), "token inexistente")
}

// TestRefreshConcurrentReuse garante que N Refresh concorrentes com o mesmo
// token resultam em exatamente um par de tokens novo.
func TestRefreshConcurrentReuse(t *testing.T) {
	e := setupRefreshEnv(t)
	raw := e.newToken(t, time.Now().Add(time.Hour))

	const n = 10
	codes := make(chan int, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			body, _ := json.Marshal(map[string]string{"refresh_token": raw})
			resp, err := http.Post(e.srv.URL+"/auth/refresh", "application/json", bytes.NewReader(body))
			if err != nil {
				codes <- -1
				return
			}
			resp.Body.Close()
			codes <- resp.StatusCode
		}()
	}
	close(start)
	wg.Wait()
	close(codes)

	counts := map[int]int{}
	for c := range codes {
		counts[c]++
	}
	assert.Equal(t, map[int]int{http.StatusOK: 1, http.StatusUnauthorized: n - 1}, counts)
}
