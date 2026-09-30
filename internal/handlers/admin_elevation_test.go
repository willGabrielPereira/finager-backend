package handlers_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

// Testes de step-up auth (elevação de sessão) para a área administrativa:
// Service.GenerateElevatedToken, Handler.Elevate (POST /auth/elevate) e o
// middleware RequireElevated (header X-Admin-Elevation) exigido em todas as
// rotas /admin/*, na cadeia DEPOIS de RequireRole.

// — Caso (a): rota /admin/* sem o header X-Admin-Elevation ──────────────────

func TestElevation_AccessTokenSemHeaderElevacao_RetornaElevationRequired(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-elev-a", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	admin := createTestUser(t, ctx, repos, "admin")
	token, err := authSvc.GenerateToken(admin)
	require.NoError(t, err)

	// Sem X-Admin-Elevation: mesmo tendo role=admin e access token válido, deve
	// ser barrado com o código específico de elevação exigida.
	rr := adminRequest(t, mux, "GET", "/admin/stats/overview", token, nil)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"code":"E_ELEVATION_REQUIRED"`)
}

// — Caso (b): token elevado inválido (malformado / assinado com chave errada) ─
// e, por contraste, um token elevado legítimo funcionando normalmente.

func TestElevation_TokenElevadoInvalido_RetornaElevationRequired(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-elev-b", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	admin := createTestUser(t, ctx, repos, "admin")
	token, err := authSvc.GenerateToken(admin)
	require.NoError(t, err)

	t.Run("Token elevado malformado retorna E_ELEVATION_REQUIRED", func(t *testing.T) {
		rr := adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, "isto-nao-e-um-jwt", nil)
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"code":"E_ELEVATION_REQUIRED"`)
	})

	t.Run("Token elevado assinado com chave diferente retorna E_ELEVATION_REQUIRED", func(t *testing.T) {
		outroSvc := auth.NewService("outra-chave-completamente-diferente", 1)
		wrongKeyElevated, err := outroSvc.GenerateElevatedToken(admin)
		require.NoError(t, err)

		rr := adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, wrongKeyElevated, nil)
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"code":"E_ELEVATION_REQUIRED"`)
	})

	t.Run("Token elevado válido (mesmo usuário, mesma chave) funciona normalmente", func(t *testing.T) {
		elevated := generateElevatedToken(t, authSvc, admin)
		rr := adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})

	t.Run("Token elevado expirado retorna E_ELEVATION_REQUIRED", func(t *testing.T) {
		// Mesmo formato/claims/chave de um token elevado real, só com ExpiresAt no
		// passado — simula o TTL de 15min de GenerateElevatedToken já ter vencido.
		expiredClaims := auth.Claims{
			UserID:   admin.ID.String(),
			Login:    admin.Login,
			FamilyID: admin.FamilyID.String(),
			RegisteredClaims: jwt.RegisteredClaims{
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-20 * time.Minute)),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-5 * time.Minute)),
				Audience:  jwt.ClaimStrings{"admin"},
			},
		}
		expiredToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims).SignedString([]byte("teste-secret-elev-b"))
		require.NoError(t, err)

		rr := adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, expiredToken, nil)
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"code":"E_ELEVATION_REQUIRED"`)
	})
}

// — Caso (c): token elevado de um usuário DIFERENTE do access token ──────────

func TestElevation_TokenElevadoDeOutroUsuario_RetornaElevationRequired(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-elev-c", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	adminA := createTestUser(t, ctx, repos, "admin")
	adminB := createTestUser(t, ctx, repos, "admin")

	tokenA, err := authSvc.GenerateToken(adminA)
	require.NoError(t, err)
	elevatedB := generateElevatedToken(t, authSvc, adminB)

	// Access token de A + token elevado de B: RequireElevated compara o UserID
	// do token elevado com o do access token da requisição e deve barrar.
	rr := adminRequestElevated(t, mux, "GET", "/admin/stats/overview", tokenA, elevatedB, nil)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"code":"E_ELEVATION_REQUIRED"`)
}

// — Caso (d): token elevado usado no header Authorization contra rota comum ──

func TestElevation_TokenElevadoNoHeaderAuthorization_RetornaUnauthorized(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-elev-d", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	admin := createTestUser(t, ctx, repos, "admin")
	elevated := generateElevatedToken(t, authSvc, admin)

	// O token elevado (aud=["admin"]) enviado como Bearer comum contra /me deve
	// ser rejeitado pelo próprio Authenticate — nunca serve de token de sessão.
	rr := adminRequest(t, mux, "GET", "/me", elevated, nil)
	assert.Equal(t, http.StatusUnauthorized, rr.Code, rr.Body.String())
}

// — Caso (e): POST /auth/elevate com senha errada e rate limit ───────────────

func TestElevation_PostAuthElevate_SenhaErradaERateLimit(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-elev-e", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	// Usuário exclusivo deste teste: o elevateLimiter criado dentro de
	// newAdminMux é por instância de mux, mas mesmo assim isolamos por login
	// próprio para não interferir com outros subtestes que usam o mesmo mux.
	admin := createTestUser(t, ctx, repos, "admin")
	token, err := authSvc.GenerateToken(admin)
	require.NoError(t, err)

	t.Run("Senha errada retorna 403 E_INVALID_PASSWORD (não 401)", func(t *testing.T) {
		rr := adminRequest(t, mux, "POST", "/auth/elevate", token, map[string]string{"password": "senha-errada-com-certeza"})
		require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), `"code":"E_INVALID_PASSWORD"`)
	})

	t.Run("Mais de 10 tentativas na janela retornam 429 E_RATE_LIMITED em algum momento", func(t *testing.T) {
		var got429 bool
		for i := 0; i < 15; i++ {
			rr := adminRequest(t, mux, "POST", "/auth/elevate", token, map[string]string{"password": "senha-errada-com-certeza"})
			if rr.Code == http.StatusTooManyRequests {
				assert.Contains(t, rr.Body.String(), `"code":"E_RATE_LIMITED"`)
				got429 = true
				break
			}
			require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
		}
		assert.True(t, got429, "esperava pelo menos uma resposta 429 E_RATE_LIMITED após 15 tentativas")
	})
}

// — Caso (f): usuário role=user chamando rota /admin/* ────────────────────────

func TestElevation_UsuarioComumChamandoRotaAdmin_RetornaForbiddenDeRoleSemCodigoDeElevacao(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-elev-f", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	user := createTestUser(t, ctx, repos, "user")
	token, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	// Sem token elevado: RequireRole deve barrar antes de chegar em RequireElevated.
	rr := adminRequest(t, mux, "GET", "/admin/stats/overview", token, nil)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), "E_ELEVATION_REQUIRED", "RequireRole deve rodar antes de RequireElevated")

	// Mesmo enviando um token elevado (de outro admin, só pra ver se "ajuda" —
	// não deveria), o resultado continua sendo 403 de papel, não de elevação.
	otherAdmin := createTestUser(t, ctx, repos, "admin")
	elevatedFromAdmin := generateElevatedToken(t, authSvc, otherAdmin)
	rr = adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, elevatedFromAdmin, nil)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), "E_ELEVATION_REQUIRED")
}

// — Caso (g): admin rebaixado a user enquanto ainda tem token elevado válido ──

func TestElevation_AdminRebaixadoComTokenElevadoValido_AindaRecebeForbiddenDeRole(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-elev-g", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	// Precisa de um segundo admin para o rebaixamento não ser bloqueado pela
	// proteção de "último admin" (ver TestAdminLastAdminProtection).
	_ = createTestUser(t, ctx, repos, "admin")
	admin := createTestUser(t, ctx, repos, "admin")

	token, err := authSvc.GenerateToken(admin)
	require.NoError(t, err)
	elevated := generateElevatedToken(t, authSvc, admin)

	// Confirma que, ainda como admin, a rota funciona normalmente.
	rr := adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, elevated, nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	// Rebaixa via repositório (RequireRole busca fresco no banco a cada request).
	require.NoError(t, repos.Users.UpdateRole(ctx, admin.ID, "user"))

	// Mesmo access token + mesmo token elevado (ainda dentro da validade) agora
	// deve ser barrado por papel insuficiente, não por elevação.
	rr = adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, elevated, nil)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.NotContains(t, rr.Body.String(), "E_ELEVATION_REQUIRED")
}

// — Caso (h): logout com token elevado no header blocklista-o ────────────────

func TestElevation_LogoutComTokenElevado_BlocklistaOToken(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-elev-h", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	admin := createTestUser(t, ctx, repos, "admin")
	token, err := authSvc.GenerateToken(admin)
	require.NoError(t, err)
	elevated := generateElevatedToken(t, authSvc, admin)

	// Confirma que o token elevado funciona antes do logout.
	rr := adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, elevated, nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	// Logout enviando o token elevado no header X-Admin-Elevation. Isso também
	// blocklista o access token da requisição de logout (comportamento normal
	// do Logout), então a tentativa seguinte usa um access token NOVO — o alvo
	// deste teste é confirmar que o token ELEVADO foi blocklistado, não o access token.
	rr = adminRequestElevated(t, mux, "POST", "/auth/logout", token, elevated, nil)
	require.Equal(t, http.StatusNoContent, rr.Code, rr.Body.String())

	// JWTs codificam IssuedAt em resolução de segundo e não carregam jti, então
	// gerar um novo token no MESMO segundo do anterior produziria a mesma
	// assinatura (o próprio token que acabou de ser blocklistado). Espera 1s
	// para garantir um access token realmente distinto.
	time.Sleep(1100 * time.Millisecond)
	newToken, err := authSvc.GenerateToken(admin)
	require.NoError(t, err)

	// O MESMO token elevado agora deve estar blocklistado.
	rr = adminRequestElevated(t, mux, "GET", "/admin/stats/overview", newToken, elevated, nil)
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"code":"E_ELEVATION_REQUIRED"`)
}

// — Caso (i): POST /auth/elevate chamado por usuário role=user ───────────────

func TestElevation_PostAuthElevate_UsuarioComum_RetornaForbiddenRoleSemToken(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-elev-i", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	user := createTestUser(t, ctx, repos, "user")
	token, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	rr := adminRequest(t, mux, "POST", "/auth/elevate", token, map[string]string{"password": "qualquer-coisa"})
	require.Equal(t, http.StatusForbidden, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"code":"E_FORBIDDEN_ROLE"`)
	assert.NotContains(t, rr.Body.String(), "elevated_token", "nenhum token deve ser retornado para papel insuficiente")
}

