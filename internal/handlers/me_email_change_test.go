package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

const profilePassword = "senha-perfil-123"

type profileEnv struct {
	mux     *http.ServeMux
	repos   *repository.Container
	authSvc *auth.Service
	sender  *fakeSender
}

type profileUser struct {
	ID       uuid.UUID
	FamilyID uuid.UUID
	Login    string
	Email    string
	Token    string
}

func setupProfileEnv(t *testing.T) *profileEnv {
	t.Helper()
	db, cleanup := testutil.SetupPostgresContainer(t)
	t.Cleanup(cleanup)
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-profile", 1)
	sender := &fakeSender{}
	ph := handlers.NewProfileHandler(repos.Users, repos.Families, sender)
	ah := auth.NewHandler(authSvc, repos.Users, repos.Families, repos.RefreshTokens, repos.Blocklist, repos.Invites, repos.SignupInvites, 1, middleware.NewInMemoryRateLimiter(10, time.Minute))

	mux := http.NewServeMux()
	mux.Handle("PUT /me", middleware.Authenticate(authSvc, repos.Blocklist)(http.HandlerFunc(ph.Update)))
	mux.Handle("POST /auth/register", http.HandlerFunc(ah.Register))
	return &profileEnv{mux: mux, repos: repos, authSvc: authSvc, sender: sender}
}

func (e *profileEnv) createUser(t *testing.T) profileUser {
	t.Helper()
	ctx := context.Background()
	fam := &models.Family{Name: "Família Perfil"}
	require.NoError(t, e.repos.Families.Create(ctx, fam))
	hash, err := bcrypt.GenerateFromPassword([]byte(profilePassword), bcrypt.MinCost)
	require.NoError(t, err)
	suffix := uuid.NewString()[:8]
	user := &models.User{Login: "perfil_" + suffix, Email: "antigo_" + suffix + "@example.com", PasswordHash: string(hash), FamilyID: fam.ID}
	require.NoError(t, e.repos.Users.Create(ctx, user))
	require.NoError(t, e.repos.Families.AddMember(ctx, fam.ID, user.ID))
	token, err := e.authSvc.GenerateToken(user)
	require.NoError(t, err)
	return profileUser{ID: user.ID, FamilyID: fam.ID, Login: user.Login, Email: user.Email, Token: token}
}

func (e *profileEnv) do(t *testing.T, method, path, token string, body any) (int, string) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func (e *profileEnv) put(t *testing.T, u profileUser, body map[string]string) (int, string) {
	return e.do(t, "PUT", "/me", u.Token, body)
}

// assertUnchanged confere que login, e-mail e nome da família continuam os originais.
func (e *profileEnv) assertUnchanged(t *testing.T, u profileUser) {
	t.Helper()
	ctx := context.Background()
	got, err := e.repos.Users.FindByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, u.Login, got.Login)
	assert.Equal(t, u.Email, got.Email)
	fam, err := e.repos.Families.FindByID(ctx, u.FamilyID)
	require.NoError(t, err)
	assert.Equal(t, "Família Perfil", fam.Name)
}

func (e *profileEnv) assertNoEmail(t *testing.T) {
	t.Helper()
	require.Never(t, func() bool { return e.sender.callCount() > 0 }, neverWindow, 20*time.Millisecond)
}

func (e *profileEnv) emailOf(t *testing.T, u profileUser) string {
	t.Helper()
	got, err := e.repos.Users.FindByID(context.Background(), u.ID)
	require.NoError(t, err)
	return got.Email
}

func TestEmailChangeRequiresPassword(t *testing.T) {
	e := setupProfileEnv(t)
	u := e.createUser(t)
	newEmail := "novo_" + uuid.NewString()[:8] + "@example.com"

	code, body := e.put(t, u, map[string]string{"email": newEmail, "login": "login_novo_1"})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Contains(t, body, "E_PASSWORD_REQUIRED")

	code, body = e.put(t, u, map[string]string{"email": newEmail, "login": "login_novo_1", "current_password": "errada"})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Contains(t, body, "E_INVALID_PASSWORD")

	e.assertUnchanged(t, u)
	e.assertNoEmail(t)
}

func TestEmailChangeNotifiesOldAddress(t *testing.T) {
	e := setupProfileEnv(t)
	u := e.createUser(t)
	newEmail := "novo_" + uuid.NewString()[:8] + "@example.com"

	code, _ := e.put(t, u, map[string]string{"email": newEmail, "current_password": profilePassword})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, newEmail, e.emailOf(t, u))

	require.Eventually(t, func() bool { return len(e.sender.sent()) == 1 }, 2*time.Second, 10*time.Millisecond)
	msgs := e.sender.sentTo(u.Email)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].HTML, "n***@example.com")
	assert.NotContains(t, msgs[0].HTML, newEmail)
	assert.Empty(t, e.sender.sentTo(newEmail))
	require.Never(t, func() bool { return e.sender.callCount() > 1 }, neverWindow, 20*time.Millisecond)
}

func TestEmailChangeSameEmailNoPasswordNeeded(t *testing.T) {
	e := setupProfileEnv(t)
	u := e.createUser(t)

	code, _ := e.put(t, u, map[string]string{"email": strings.ToUpper(u.Email)})
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, u.Email, e.emailOf(t, u))
	e.assertNoEmail(t)
}

func TestEmailChangeRejectsInvalidEmail(t *testing.T) {
	e := setupProfileEnv(t)
	u := e.createUser(t)

	for _, bad := range []string{"a b@c.d", "x@.com", "sem-arroba.com"} {
		code, _ := e.put(t, u, map[string]string{"email": bad, "current_password": profilePassword, "login": "login_novo_2"})
		assert.Equal(t, http.StatusUnprocessableEntity, code, bad)
	}
	e.assertUnchanged(t, u)
	e.assertNoEmail(t)

	// Espaços nas bordas: normalização roda antes da validação, então é aceito.
	code, _ := e.put(t, u, map[string]string{"email": " Nome@X.com ", "current_password": profilePassword})
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "nome@x.com", e.emailOf(t, u))
}

func TestFamilyNameMaxLength(t *testing.T) {
	e := setupProfileEnv(t)
	name60 := strings.Repeat("é", 60) // 120 bytes, 60 runas
	name61 := strings.Repeat("é", 61)

	t.Run("PUT /me", func(t *testing.T) {
		u := e.createUser(t)
		code, _ := e.put(t, u, map[string]string{"family_name": name61})
		assert.Equal(t, http.StatusUnprocessableEntity, code)
		e.assertUnchanged(t, u)

		code, _ = e.put(t, u, map[string]string{"family_name": name60})
		assert.Equal(t, http.StatusOK, code)
		fam, err := e.repos.Families.FindByID(context.Background(), u.FamilyID)
		require.NoError(t, err)
		assert.Equal(t, name60, fam.Name)
	})

	t.Run("cadastro", func(t *testing.T) {
		reg := func(name string) int {
			s := uuid.NewString()[:8]
			code, _ := e.do(t, "POST", "/auth/register", "", map[string]string{
				"login": "cad_" + s, "email": "cad_" + s + "@example.com", "password": "senha-cadastro-1", "family_name": name,
			})
			return code
		}
		assert.Equal(t, http.StatusUnprocessableEntity, reg(name61))
		assert.Equal(t, http.StatusCreated, reg(name60))
	})
}

func TestProfileUpdateIsAtomicOnValidationError(t *testing.T) {
	e := setupProfileEnv(t)
	u := e.createUser(t)

	// Login e e-mail válidos, nome da família inválido: nada pode ser gravado.
	code, _ := e.put(t, u, map[string]string{
		"login": "login_atomico", "email": "atomico_" + uuid.NewString()[:8] + "@example.com",
		"current_password": profilePassword, "family_name": "x",
	})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	e.assertUnchanged(t, u)

	// Login e família válidos, e-mail inválido: nada pode ser gravado.
	code, _ = e.put(t, u, map[string]string{"login": "login_atomico", "family_name": "Família Nova", "email": "invalido"})
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	e.assertUnchanged(t, u)
	e.assertNoEmail(t)
}

func TestEmailChangeSenderFailureStill200(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		panics bool
	}{
		{"erro", errors.New("provedor fora do ar"), false},
		{"panic", nil, true},
	}
	e := setupProfileEnv(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.sender.set(tc.err, tc.panics)
			before := e.sender.callCount()
			u := e.createUser(t)
			newEmail := "falha_" + uuid.NewString()[:8] + "@example.com"

			code, _ := e.put(t, u, map[string]string{"email": newEmail, "current_password": profilePassword})
			assert.Equal(t, http.StatusOK, code)
			assert.Equal(t, newEmail, e.emailOf(t, u))
			// O envio foi tentado (e falhou) sem derrubar o processo.
			require.Eventually(t, func() bool { return e.sender.callCount() == before+1 }, 2*time.Second, 10*time.Millisecond)
		})
	}
}
