package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/billing"
	"github.com/willGabrielPereira/finager-backend/internal/handlers"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

// — Helpers compartilhados pelos testes de admin ────────────────────────────

// newAdminMux monta um mux com as rotas /admin/* (com os middlewares reais de
// autenticação e role) e POST /auth/register, replicando exatamente o que
// cmd/api/routes.go registra em produção para essas rotas.
func newAdminMux(repos *repository.Container, authSvc *auth.Service) http.Handler {
	authMid := middleware.Authenticate(authSvc, repos.Blocklist)
	adminOrModMid := middleware.RequireRole(repos.Users, "admin", "moderator")
	adminOnlyMid := middleware.RequireRole(repos.Users, "admin")
	// Exige reautenticação recente (step-up), igual à cadeia real em cmd/api/routes.go.
	elevatedMid := middleware.RequireElevated(authSvc, repos.Blocklist)

	adminHandler := handlers.NewAdminHandler(repos.Users, repos.Coupons, repos.SignupInvites)
	authHandler := auth.NewHandler(authSvc, repos.Users, repos.Families, repos.RefreshTokens, repos.Blocklist, repos.Invites, repos.SignupInvites, 1, middleware.NewInMemoryRateLimiter(10, time.Minute))
	profileHandler := handlers.NewProfileHandler(repos.Users, repos.Families)

	mux := http.NewServeMux()
	mux.Handle("POST /auth/register", http.HandlerFunc(authHandler.Register))
	mux.Handle("POST /auth/elevate", authMid(http.HandlerFunc(authHandler.Elevate)))
	mux.Handle("POST /auth/logout", authMid(http.HandlerFunc(authHandler.Logout)))
	mux.Handle("GET /me", authMid(http.HandlerFunc(profileHandler.Get)))

	mux.Handle("GET /admin/stats/overview", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.Overview)))))
	mux.Handle("GET /admin/stats/activity", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.Activity)))))
	mux.Handle("GET /admin/users", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.ListAdmins)))))
	mux.Handle("PATCH /admin/users/{id}/role", authMid(adminOnlyMid(elevatedMid(http.HandlerFunc(adminHandler.UpdateUserRole)))))
	mux.Handle("POST /admin/invites", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.CreateInvite)))))
	mux.Handle("GET /admin/invites", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.ListInvites)))))
	mux.Handle("DELETE /admin/invites/{id}", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.RevokeInvite)))))
	mux.Handle("GET /admin/coupons", authMid(adminOnlyMid(elevatedMid(http.HandlerFunc(adminHandler.ListCoupons)))))
	mux.Handle("POST /admin/coupons", authMid(adminOnlyMid(elevatedMid(http.HandlerFunc(adminHandler.CreateCoupon)))))
	mux.Handle("PATCH /admin/coupons/{id}", authMid(adminOnlyMid(elevatedMid(http.HandlerFunc(adminHandler.UpdateCoupon)))))

	return mux
}

// adminRequest monta e dispara uma requisição HTTP contra o mux de teste,
// serializando body (quando informado) como JSON e injetando o Bearer token.
func adminRequest(t *testing.T, mux http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		req = httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// adminRequestElevated é como adminRequest, mas também envia o header
// X-Admin-Elevation com um token de step-up — necessário em toda rota
// /admin/* desde a introdução do RequireElevated.
func adminRequestElevated(t *testing.T, mux http.Handler, method, path, token, elevatedToken string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		req = httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if elevatedToken != "" {
		req.Header.Set("X-Admin-Elevation", elevatedToken)
	}

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// generateElevatedToken gera um token elevado (step-up) direto via o Service,
// sem passar pelo fluxo HTTP de reautenticação por senha — usado pelos testes
// que só precisam simular uma sessão administrativa já elevada.
func generateElevatedToken(t *testing.T, authSvc *auth.Service, user *models.User) string {
	t.Helper()
	token, err := authSvc.GenerateElevatedToken(user)
	require.NoError(t, err)
	return token
}

// createTestUser cria uma família + usuário isolados (próprios do teste) e,
// se necessário, promove o usuário via UpdateRole (não há endpoint para criar
// um admin/moderator diretamente, então promovemos via repositório).
func createTestUser(t *testing.T, ctx context.Context, repos *repository.Container, role string) *models.User {
	t.Helper()

	fam := &models.Family{Name: "Família " + uuid.NewString()}
	require.NoError(t, repos.Families.Create(ctx, fam))

	user := &models.User{
		Login:        "user_" + uuid.NewString()[:8],
		Email:        uuid.NewString() + "@example.com",
		PasswordHash: "hash-nao-usado-neste-teste",
		FamilyID:     fam.ID,
	}
	require.NoError(t, repos.Users.Create(ctx, user))
	require.NoError(t, repos.Families.AddMember(ctx, fam.ID, user.ID))

	if role != "" && role != "user" {
		require.NoError(t, repos.Users.UpdateRole(ctx, user.ID, role))
		user.Role = role
	}
	return user
}

// createSignupInvite insere um convite de cadastro diretamente via
// repositório, evitando depender do endpoint POST /admin/invites nos testes
// que não estão focados nessa rota especificamente.
func createSignupInvite(t *testing.T, ctx context.Context, repos *repository.Container, createdBy uuid.UUID, expiresAt time.Time) *models.SignupInvite {
	t.Helper()

	token, err := models.GenerateSignupInviteToken()
	require.NoError(t, err)

	invite := &models.SignupInvite{
		Token:       token,
		PlanGranted: models.PlanLifetimeFree,
		CreatedBy:   createdBy,
		ExpiresAt:   expiresAt,
	}
	require.NoError(t, repos.SignupInvites.Create(ctx, invite))
	return invite
}

// — Caso 1, 2 e 3: matriz de controle de acesso por role ────────────────────

func TestAdminAccessControlByRole(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)
	require.NoError(t, repos.EnsureIndexes(context.Background()))

	authSvc := auth.NewService("teste-secret-admin", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	t.Run("Usuário comum (role=user) recebe 403 em todas as rotas /admin/*", func(t *testing.T) {
		user := createTestUser(t, ctx, repos, "user")
		token, err := authSvc.GenerateToken(user)
		require.NoError(t, err)

		routes := []struct {
			method string
			path   string
			body   any
		}{
			{"GET", "/admin/stats/overview", nil},
			{"GET", "/admin/stats/activity", nil},
			{"GET", "/admin/users", nil},
			{"PATCH", "/admin/users/" + uuid.NewString() + "/role", map[string]string{"role": "moderator"}},
			{"POST", "/admin/invites", nil},
			{"GET", "/admin/invites", nil},
			{"DELETE", "/admin/invites/" + uuid.NewString(), nil},
			{"GET", "/admin/coupons", nil},
			{"POST", "/admin/coupons", map[string]any{"code": "TESTE", "discount_percent": 10, "plan_granted": models.PlanPro}},
			{"PATCH", "/admin/coupons/" + uuid.NewString(), map[string]any{"active": false}},
		}

		for _, route := range routes {
			rr := adminRequest(t, mux, route.method, route.path, token, route.body)
			assert.Equalf(t, http.StatusForbidden, rr.Code, "%s %s deveria retornar 403 para role=user", route.method, route.path)
		}
	})

	t.Run("Moderator tem acesso parcial: sucesso nas rotas de estatísticas/convites, 403 em cupons e promoção de role", func(t *testing.T) {
		mod := createTestUser(t, ctx, repos, "moderator")
		token, err := authSvc.GenerateToken(mod)
		require.NoError(t, err)
		elevated := generateElevatedToken(t, authSvc, mod)

		// Rotas permitidas para moderator (adminOrModMid), agora também exigindo
		// step-up (elevatedMid) — sem o header, mesmo um moderator tomaria 403.
		rr := adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "GET", "/admin/stats/activity", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "GET", "/admin/users", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "POST", "/admin/invites", token, elevated, nil)
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var invite models.SignupInvite
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&invite))

		rr = adminRequestElevated(t, mux, "GET", "/admin/invites", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "DELETE", "/admin/invites/"+invite.ID.String(), token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		// Rotas restritas a admin (adminOnlyMid) — moderator deve ser barrado
		rr = adminRequest(t, mux, "PATCH", "/admin/users/"+uuid.NewString()+"/role", token, map[string]string{"role": "admin"})
		assert.Equal(t, http.StatusForbidden, rr.Code)

		rr = adminRequest(t, mux, "GET", "/admin/coupons", token, nil)
		assert.Equal(t, http.StatusForbidden, rr.Code)

		rr = adminRequest(t, mux, "POST", "/admin/coupons", token, map[string]any{"code": "MODTEST", "discount_percent": 10, "plan_granted": models.PlanPro})
		assert.Equal(t, http.StatusForbidden, rr.Code)

		rr = adminRequest(t, mux, "PATCH", "/admin/coupons/"+uuid.NewString(), token, map[string]any{"active": false})
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("Admin tem sucesso em todas as rotas /admin/*", func(t *testing.T) {
		admin := createTestUser(t, ctx, repos, "admin")
		target := createTestUser(t, ctx, repos, "user")
		token, err := authSvc.GenerateToken(admin)
		require.NoError(t, err)
		elevated := generateElevatedToken(t, authSvc, admin)

		rr := adminRequestElevated(t, mux, "GET", "/admin/stats/overview", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "GET", "/admin/stats/activity", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "GET", "/admin/users", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		// Promove o usuário-alvo (não é admin, então a proteção de "último admin" não se aplica)
		rr = adminRequestElevated(t, mux, "PATCH", "/admin/users/"+target.ID.String()+"/role", token, elevated, map[string]string{"role": "moderator"})
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "POST", "/admin/invites", token, elevated, nil)
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var invite models.SignupInvite
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&invite))

		rr = adminRequestElevated(t, mux, "GET", "/admin/invites", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "DELETE", "/admin/invites/"+invite.ID.String(), token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "GET", "/admin/coupons", token, elevated, nil)
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		rr = adminRequestElevated(t, mux, "POST", "/admin/coupons", token, elevated, map[string]any{"code": "ADMINTEST" + uuid.NewString()[:6], "discount_percent": 50, "plan_granted": models.PlanLifetimeFree})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var coupon models.Coupon
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&coupon))

		rr = adminRequestElevated(t, mux, "PATCH", "/admin/coupons/"+coupon.ID.String(), token, elevated, map[string]any{"active": false})
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	})
}

// — Caso 4: fluxo completo de convite de cadastro (SignupInvite) ────────────

func TestAdminSignupInviteFlow(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)
	require.NoError(t, repos.EnsureIndexes(context.Background()))

	authSvc := auth.NewService("teste-secret-invite", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	t.Run("Convite de cadastro concede plano LIFETIME_FREE, é marcado como usado, e não pode ser reutilizado", func(t *testing.T) {
		admin := createTestUser(t, ctx, repos, "admin")
		invite := createSignupInvite(t, ctx, repos, admin.ID, time.Now().Add(24*time.Hour))

		login := "novo_" + uuid.NewString()[:8]
		email := uuid.NewString() + "@example.com"

		registerBody := map[string]string{
			"login":        login,
			"email":        email,
			"password":     "SenhaForte123",
			"invite_token": invite.Token,
		}

		rr := adminRequest(t, mux, "POST", "/auth/register", "", registerBody)
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())

		// Confirma que a família recém-criada recebeu o plano vitalício gratuito
		createdUser, err := repos.Users.FindByLogin(ctx, login)
		require.NoError(t, err)
		require.NotNil(t, createdUser)

		family, err := repos.Families.FindByID(ctx, createdUser.FamilyID)
		require.NoError(t, err)
		assert.Equal(t, models.PlanLifetimeFree, family.Plan)

		// Confirma que o convite foi marcado como usado por essa família
		usedInvite, err := repos.SignupInvites.GetByToken(ctx, invite.Token)
		require.NoError(t, err)
		require.NotNil(t, usedInvite.UsedAt)
		require.NotNil(t, usedInvite.UsedByFamilyID)
		assert.Equal(t, createdUser.FamilyID, *usedInvite.UsedByFamilyID)

		// Tenta registrar OUTRA conta reutilizando o MESMO token: deve falhar.
		// O handler trata token já usado como erro de validação do campo
		// invite_token (mesmo tratamento dado a "expirado"/"inválido"), retornando 422.
		secondBody := map[string]string{
			"login":        "outro_" + uuid.NewString()[:8],
			"email":        uuid.NewString() + "@example.com",
			"password":     "OutraSenha123",
			"invite_token": invite.Token,
		}
		rr = adminRequest(t, mux, "POST", "/auth/register", "", secondBody)
		assert.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())

		// A segunda conta não deve ter sido criada
		secondUser, err := repos.Users.FindByLogin(ctx, secondBody["login"])
		assert.Error(t, err)
		assert.Nil(t, secondUser)
	})

	t.Run("Convite de cadastro expirado é rejeitado no registro", func(t *testing.T) {
		admin := createTestUser(t, ctx, repos, "admin")
		expiredInvite := createSignupInvite(t, ctx, repos, admin.ID, time.Now().Add(-1*time.Hour))

		login := "expirado_" + uuid.NewString()[:8]
		registerBody := map[string]string{
			"login":        login,
			"email":        uuid.NewString() + "@example.com",
			"password":     "SenhaForte123",
			"invite_token": expiredInvite.Token,
		}

		rr := adminRequest(t, mux, "POST", "/auth/register", "", registerBody)
		assert.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())

		createdUser, err := repos.Users.FindByLogin(ctx, login)
		assert.Error(t, err)
		assert.Nil(t, createdUser)
	})
}

// — Caso 5: proteção contra remoção do último admin ─────────────────────────

func TestAdminLastAdminProtection(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)
	require.NoError(t, repos.EnsureIndexes(context.Background()))

	authSvc := auth.NewService("teste-secret-lastadmin", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	// Este primeiro subteste roda com um único admin no banco (nenhum outro
	// admin foi criado ainda neste container isolado).
	t.Run("Admin único não consegue rebaixar a si mesmo", func(t *testing.T) {
		admin := createTestUser(t, ctx, repos, "admin")
		token, err := authSvc.GenerateToken(admin)
		require.NoError(t, err)
		elevated := generateElevatedToken(t, authSvc, admin)

		rr := adminRequestElevated(t, mux, "PATCH", "/admin/users/"+admin.ID.String()+"/role", token, elevated, map[string]string{"role": "user"})
		assert.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())

		// Confirma que o role não mudou
		stillAdmin, err := repos.Users.FindByID(ctx, admin.ID)
		require.NoError(t, err)
		assert.Equal(t, "admin", stillAdmin.Role)
	})

	// A partir daqui existem 2 admins no banco (o do subteste acima + este),
	// então o rebaixamento deve funcionar normalmente.
	t.Run("Com dois admins, um consegue rebaixar o outro normalmente", func(t *testing.T) {
		adminB := createTestUser(t, ctx, repos, "admin")
		token, err := authSvc.GenerateToken(adminB)
		require.NoError(t, err)
		elevated := generateElevatedToken(t, authSvc, adminB)

		rr := adminRequestElevated(t, mux, "PATCH", "/admin/users/"+adminB.ID.String()+"/role", token, elevated, map[string]string{"role": "moderator"})
		assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		demoted, err := repos.Users.FindByID(ctx, adminB.ID)
		require.NoError(t, err)
		assert.Equal(t, "moderator", demoted.Role)
	})
}

// — Caso 6: ciclo de vida de um cupom ────────────────────────────────────────

func TestAdminCouponLifecycle(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)
	require.NoError(t, repos.EnsureIndexes(context.Background()))

	authSvc := auth.NewService("teste-secret-coupon", 1)
	mux := newAdminMux(repos, authSvc)
	ctx := context.Background()

	t.Run("Cria um cupom, confirma na listagem, desativa e confirma a mudança", func(t *testing.T) {
		admin := createTestUser(t, ctx, repos, "admin")
		token, err := authSvc.GenerateToken(admin)
		require.NoError(t, err)
		elevated := generateElevatedToken(t, authSvc, admin)

		code := "CICLO" + uuid.NewString()[:6]
		rr := adminRequestElevated(t, mux, "POST", "/admin/coupons", token, elevated, map[string]any{
			"code":             code,
			"discount_percent": 25,
			"plan_granted":     models.PlanPro,
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var created models.Coupon
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))
		assert.True(t, created.Active)

		// Confirma que aparece na listagem, ativo
		rr = adminRequestElevated(t, mux, "GET", "/admin/coupons", token, elevated, nil)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var listBeforeDeactivate []models.Coupon
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&listBeforeDeactivate))
		found := findCouponByID(listBeforeDeactivate, created.ID)
		require.NotNil(t, found)
		assert.True(t, found.Active)

		// Desativa
		rr = adminRequestElevated(t, mux, "PATCH", "/admin/coupons/"+created.ID.String(), token, elevated, map[string]any{"active": false})
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		// Confirma a mudança refletida numa nova consulta
		rr = adminRequestElevated(t, mux, "GET", "/admin/coupons", token, elevated, nil)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		var listAfterDeactivate []models.Coupon
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&listAfterDeactivate))
		foundAfter := findCouponByID(listAfterDeactivate, created.ID)
		require.NotNil(t, foundAfter)
		assert.False(t, foundAfter.Active)
	})
}

// findCouponByID busca um cupom por ID numa lista, retornando nil se ausente.
func findCouponByID(coupons []models.Coupon, id uuid.UUID) *models.Coupon {
	for i := range coupons {
		if coupons[i].ID == id {
			return &coupons[i]
		}
	}
	return nil
}

// — Caso 7: cupom desativado não pode ser resgatado ─────────────────────────

func TestAdminCouponDeactivatedCannotBeApplied(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	repos := repository.New(db)
	require.NoError(t, repos.EnsureIndexes(context.Background()))

	authSvc := auth.NewService("teste-secret-coupon-inactive", 1)
	mux := newAdminMux(repos, authSvc)
	billingSvc := billing.NewService(billing.NewMockProvider(), repos.Families, repos.Coupons)
	ctx := context.Background()

	admin := createTestUser(t, ctx, repos, "admin")
	token, err := authSvc.GenerateToken(admin)
	require.NoError(t, err)
	elevated := generateElevatedToken(t, authSvc, admin)

	t.Run("Cupom desativado é rejeitado por ApplyCoupon com ErrCouponInactive", func(t *testing.T) {
		code := "INATIVO" + uuid.NewString()[:6]
		rr := adminRequestElevated(t, mux, "POST", "/admin/coupons", token, elevated, map[string]any{
			"code":             code,
			"discount_percent": 10,
			"plan_granted":     models.PlanPro,
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var created models.Coupon
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))

		rr = adminRequestElevated(t, mux, "PATCH", "/admin/coupons/"+created.ID.String(), token, elevated, map[string]any{"active": false})
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

		target := createTestUser(t, ctx, repos, "user")
		_, err := billingSvc.ApplyCoupon(ctx, target.FamilyID, code)
		assert.ErrorIs(t, err, billing.ErrCouponInactive)
	})

	t.Run("Cupom ativo continua sendo aplicado normalmente (sem regressão)", func(t *testing.T) {
		code := "ATIVO" + uuid.NewString()[:6]
		rr := adminRequestElevated(t, mux, "POST", "/admin/coupons", token, elevated, map[string]any{
			"code":             code,
			"discount_percent": 15,
			"plan_granted":     models.PlanPro,
		})
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var created models.Coupon
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&created))
		assert.True(t, created.Active)

		target := createTestUser(t, ctx, repos, "user")
		applied, err := billingSvc.ApplyCoupon(ctx, target.FamilyID, code)
		require.NoError(t, err)
		assert.Equal(t, strings.ToUpper(code), applied.Code)

		family, err := repos.Families.FindByID(ctx, target.FamilyID)
		require.NoError(t, err)
		assert.Equal(t, models.PlanPro, family.Plan)
	})
}
