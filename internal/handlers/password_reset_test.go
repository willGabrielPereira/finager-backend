package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/mailer"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

// "Não chegou e-mail" é verificado com require.Never nesta janela: o fakeSender
// entrega de forma síncrona assim que a goroutine roda, então 500ms é folga de sobra.
const neverWindow = 500 * time.Millisecond

// fakeSender guarda as mensagens enviadas; pode ser configurado para falhar ou dar panic.
type fakeSender struct {
	mu     sync.Mutex
	msgs   []mailer.Message
	calls  int
	err    error
	panics bool
}

func (f *fakeSender) Send(_ context.Context, m mailer.Message) error {
	f.mu.Lock()
	f.calls++
	err, panics := f.err, f.panics
	if err == nil && !panics {
		f.msgs = append(f.msgs, m)
	}
	f.mu.Unlock()
	if panics {
		panic("fakeSender: panic proposital")
	}
	return err
}

func (f *fakeSender) set(err error, panics bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err, f.panics = err, panics
}

func (f *fakeSender) sent() []mailer.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mailer.Message(nil), f.msgs...)
}

func (f *fakeSender) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeSender) sentTo(addr string) []mailer.Message {
	var out []mailer.Message
	for _, m := range f.sent() {
		for _, to := range m.To {
			if to == addr {
				out = append(out, m)
			}
		}
	}
	return out
}

type resetEnv struct {
	srv     *httptest.Server
	db      *pgxpool.Pool
	repos   *repository.Container
	authSvc *auth.Service
}

type resetUser struct {
	ID       uuid.UUID
	Login    string
	Email    string
	Password string
}

// setupResetEnv sobe Postgres e expõe forgot/reset (com rate limit por IP, como em
// cmd/api/routes.go), login e refresh num httptest.Server real.
func setupResetEnv(t *testing.T, sender mailer.Sender, forgotIP, perEmail, resetIP int) *resetEnv {
	t.Helper()
	db, cleanup := testutil.SetupPostgresContainer(t)
	t.Cleanup(cleanup)
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-reset", 1)
	h := auth.NewHandler(authSvc, repos.Users, repos.Families, repos.RefreshTokens, repos.Blocklist, repos.Invites, repos.SignupInvites, 1, middleware.NewInMemoryRateLimiter(10, time.Minute))
	ph := auth.NewPasswordResetHandler(authSvc, repos.Users, repos.PasswordResets, repos.RefreshTokens, sender,
		middleware.NewInMemoryRateLimiter(perEmail, time.Hour), "http://front.test")

	mux := http.NewServeMux()
	mux.Handle("POST /auth/password/forgot",
		middleware.RateLimit(middleware.NewInMemoryRateLimiter(forgotIP, 15*time.Minute))(http.HandlerFunc(ph.Forgot)))
	mux.Handle("POST /auth/password/reset",
		middleware.RateLimit(middleware.NewInMemoryRateLimiter(resetIP, 15*time.Minute))(http.HandlerFunc(ph.Reset)))
	mux.Handle("POST /auth/login", http.HandlerFunc(h.Login))
	mux.Handle("POST /auth/refresh", http.HandlerFunc(h.Refresh))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &resetEnv{srv: srv, db: db, repos: repos, authSvc: authSvc}
}

func setupResetEnvDefault(t *testing.T, sender mailer.Sender) *resetEnv {
	return setupResetEnv(t, sender, 1000, 1000, 1000)
}

// createUser cria um usuário numa família própria.
func (e *resetEnv) createUser(t *testing.T) resetUser {
	t.Helper()
	ctx := context.Background()
	fam := &models.Family{Name: "Família Reset " + uuid.NewString()}
	require.NoError(t, e.repos.Families.Create(ctx, fam))
	u := resetUser{Login: "reset_" + uuid.NewString()[:8], Email: "reset_" + uuid.NewString()[:8] + "@example.com", Password: "senha-antiga-123"}
	hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.MinCost)
	require.NoError(t, err)
	user := &models.User{Login: u.Login, Email: u.Email, PasswordHash: string(hash), FamilyID: fam.ID}
	require.NoError(t, e.repos.Users.Create(ctx, user))
	require.NoError(t, e.repos.Families.AddMember(ctx, fam.ID, user.ID))
	u.ID = user.ID
	return u
}

// post envia JSON; ip != "" define X-Forwarded-For (chave do rate limit por IP).
func (e *resetEnv) post(t *testing.T, path string, body any, ip string) (int, []byte) {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, e.srv.URL+path, bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, out
}

func (e *resetEnv) forgot(t *testing.T, email string) int {
	t.Helper()
	code, _ := e.post(t, "/auth/password/forgot", map[string]string{"email": email}, "")
	return code
}

func (e *resetEnv) reset(t *testing.T, token, newPassword string) (int, []byte) {
	t.Helper()
	return e.post(t, "/auth/password/reset", map[string]string{"token": token, "new_password": newPassword}, "")
}

// login devolve o status e o refresh token (vazio se falhou).
func (e *resetEnv) login(t *testing.T, login, password string) (int, string) {
	t.Helper()
	code, body := e.post(t, "/auth/login", map[string]string{"login": login, "password": password}, "")
	var resp struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.Unmarshal(body, &resp)
	return code, resp.RefreshToken
}

var tokenRe = regexp.MustCompile(`/redefinir-senha#token=([A-Za-z0-9_-]+)`)

func tokenFromHTML(t *testing.T, html string) string {
	t.Helper()
	m := tokenRe.FindStringSubmatch(html)
	require.NotNil(t, m, "link de redefinição não encontrado no e-mail")
	return m[1]
}

// waitToken espera a n-ésima mensagem para addr e devolve o token dela.
func waitToken(t *testing.T, fs *fakeSender, addr string, n int) string {
	t.Helper()
	require.Eventually(t, func() bool { return len(fs.sentTo(addr)) >= n }, 5*time.Second, 10*time.Millisecond)
	return tokenFromHTML(t, fs.sentTo(addr)[n-1].HTML)
}

func TestPasswordResetForgotSameResponseForUnknownEmail(t *testing.T) {
	fs := &fakeSender{}
	e := setupResetEnvDefault(t, fs)
	u := e.createUser(t)
	unknown := "ninguem_" + uuid.NewString()[:8] + "@example.com"

	codeKnown, bodyKnown := e.post(t, "/auth/password/forgot", map[string]string{"email": u.Email}, "")
	codeUnknown, bodyUnknown := e.post(t, "/auth/password/forgot", map[string]string{"email": unknown}, "")
	assert.Equal(t, http.StatusAccepted, codeKnown)
	assert.Equal(t, codeKnown, codeUnknown)
	assert.Equal(t, string(bodyKnown), string(bodyUnknown))

	require.Eventually(t, func() bool { return len(fs.sentTo(u.Email)) == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return len(fs.sentTo(unknown)) > 0 }, neverWindow, 10*time.Millisecond)

	// Formato inválido: 422 imediato, nenhum e-mail.
	code, _ := e.post(t, "/auth/password/forgot", map[string]string{"email": "nao-e-email"}, "")
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	require.Never(t, func() bool { return len(fs.sent()) > 1 }, neverWindow, 10*time.Millisecond)
}

func TestPasswordResetTokenStoredOnlyAsHash(t *testing.T) {
	fs := &fakeSender{}
	e := setupResetEnvDefault(t, fs)
	u := e.createUser(t)

	require.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	raw := waitToken(t, fs, u.Email, 1)
	assert.Len(t, raw, 43, "32 bytes em base64 sem padding")
	assert.Contains(t, fs.sentTo(u.Email)[0].HTML, "http://front.test/redefinir-senha#token="+raw)

	rows, err := e.db.Query(context.Background(), `SELECT token_hash FROM password_reset_tokens WHERE user_id = $1`, u.ID)
	require.NoError(t, err)
	var hashes []string
	for rows.Next() {
		var h string
		require.NoError(t, rows.Scan(&h))
		hashes = append(hashes, h)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{e.authSvc.HashToken(raw)}, hashes)

	var rawCount int
	require.NoError(t, e.db.QueryRow(context.Background(), `SELECT count(*) FROM password_reset_tokens WHERE token_hash = $1`, raw).Scan(&rawCount))
	assert.Zero(t, rawCount, "token bruto nunca pode ser persistido")
}

func TestPasswordResetFlowRevokesSessions(t *testing.T) {
	fs := &fakeSender{}
	e := setupResetEnvDefault(t, fs)
	u := e.createUser(t)

	code, refresh := e.login(t, u.Login, u.Password)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, refresh)

	require.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	token := waitToken(t, fs, u.Email, 1)
	code, _ = e.reset(t, token, "senha-nova-456")
	require.Equal(t, http.StatusNoContent, code)

	code, _ = e.post(t, "/auth/refresh", map[string]string{"refresh_token": refresh}, "")
	assert.Equal(t, http.StatusUnauthorized, code, "sessão anterior deve ser revogada")
	code, _ = e.login(t, u.Login, u.Password)
	assert.Equal(t, http.StatusUnauthorized, code)
	code, _ = e.login(t, u.Login, "senha-nova-456")
	assert.Equal(t, http.StatusOK, code)

	// Reset não envia e-mail.
	require.Never(t, func() bool { return len(fs.sent()) > 1 }, neverWindow, 10*time.Millisecond)
}

func TestPasswordResetTokenSingleUse(t *testing.T) {
	fs := &fakeSender{}
	e := setupResetEnvDefault(t, fs)
	u := e.createUser(t)

	require.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	token := waitToken(t, fs, u.Email, 1)

	code, _ := e.reset(t, token, "senha-nova-456")
	require.Equal(t, http.StatusNoContent, code)
	code, body := e.reset(t, token, "outra-senha-789")
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, string(body), "E_INVALID_TOKEN")

	code, _ = e.login(t, u.Login, "senha-nova-456")
	assert.Equal(t, http.StatusOK, code, "segundo uso não pode trocar a senha")
}

func TestPasswordResetTokenExpired(t *testing.T) {
	fs := &fakeSender{}
	e := setupResetEnvDefault(t, fs)
	u := e.createUser(t)

	require.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	token := waitToken(t, fs, u.Email, 1)
	_, err := e.db.Exec(context.Background(), `UPDATE password_reset_tokens SET expires_at = now() - interval '1 minute' WHERE user_id = $1`, u.ID)
	require.NoError(t, err)

	code, body := e.reset(t, token, "senha-nova-456")
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, string(body), "E_INVALID_TOKEN")
	code, _ = e.login(t, u.Login, u.Password)
	assert.Equal(t, http.StatusOK, code, "senha não pode mudar com token expirado")
}

func TestPasswordResetNewRequestInvalidatesPrevious(t *testing.T) {
	fs := &fakeSender{}
	e := setupResetEnvDefault(t, fs)
	u := e.createUser(t)

	require.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	first := waitToken(t, fs, u.Email, 1)
	require.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	second := waitToken(t, fs, u.Email, 2)
	require.NotEqual(t, first, second)

	var n int
	require.NoError(t, e.db.QueryRow(context.Background(), `SELECT count(*) FROM password_reset_tokens WHERE user_id = $1`, u.ID).Scan(&n))
	assert.Equal(t, 1, n)

	code, _ := e.reset(t, first, "senha-nova-456")
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = e.reset(t, second, "senha-nova-456")
	assert.Equal(t, http.StatusNoContent, code)
}

func TestPasswordResetRateLimit(t *testing.T) {
	fs := &fakeSender{}
	// Mesmos limites de cmd/api/routes.go.
	e := setupResetEnv(t, fs, 5, 3, 10)
	u := e.createUser(t)

	// forgot: 5 por IP.
	for i := range 5 {
		code, _ := e.post(t, "/auth/password/forgot", map[string]string{"email": "x" + uuid.NewString()[:6] + "@example.com"}, "10.0.0.1")
		require.Equal(t, http.StatusAccepted, code, "requisição %d", i+1)
	}
	code, _ := e.post(t, "/auth/password/forgot", map[string]string{"email": u.Email}, "10.0.0.1")
	assert.Equal(t, http.StatusTooManyRequests, code)

	// forgot: 3 e-mails por endereço, mesmo vindo de IPs diferentes (resposta segue 202).
	for i := range 4 {
		code, _ := e.post(t, "/auth/password/forgot", map[string]string{"email": u.Email}, "10.0.1."+string(rune('1'+i)))
		require.Equal(t, http.StatusAccepted, code)
	}
	require.Eventually(t, func() bool { return len(fs.sentTo(u.Email)) == 3 }, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return len(fs.sentTo(u.Email)) > 3 }, neverWindow, 10*time.Millisecond)

	// reset: 10 por IP.
	for i := range 10 {
		code, _ := e.post(t, "/auth/password/reset", map[string]string{"token": "invalido", "new_password": "senha-nova-456"}, "10.0.2.1")
		require.Equal(t, http.StatusBadRequest, code, "requisição %d", i+1)
	}
	code, _ = e.post(t, "/auth/password/reset", map[string]string{"token": "invalido", "new_password": "senha-nova-456"}, "10.0.2.1")
	assert.Equal(t, http.StatusTooManyRequests, code)
}

func TestPasswordResetIsolation(t *testing.T) {
	fs := &fakeSender{}
	e := setupResetEnvDefault(t, fs)
	a := e.createUser(t) // família A
	b := e.createUser(t) // família B

	code, refreshB := e.login(t, b.Login, b.Password)
	require.Equal(t, http.StatusOK, code)

	require.Equal(t, http.StatusAccepted, e.forgot(t, a.Email))
	token := waitToken(t, fs, a.Email, 1)
	code, _ = e.reset(t, token, "senha-nova-456")
	require.Equal(t, http.StatusNoContent, code)

	// Usuário da família B intacto: senha antiga vale, nova não, sessão continua viva.
	code, _ = e.login(t, b.Login, b.Password)
	assert.Equal(t, http.StatusOK, code)
	code, _ = e.login(t, b.Login, "senha-nova-456")
	assert.Equal(t, http.StatusUnauthorized, code)
	code, _ = e.post(t, "/auth/refresh", map[string]string{"refresh_token": refreshB}, "")
	assert.Equal(t, http.StatusOK, code)
	assert.Empty(t, fs.sentTo(b.Email))

	// Token de A não serve para mais nada (nem para B).
	code, _ = e.reset(t, token, "mais-uma-senha-1")
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestPasswordResetForgotSenderFailureStill202(t *testing.T) {
	fs := &fakeSender{}
	e := setupResetEnvDefault(t, fs)
	u := e.createUser(t)

	fs.set(errors.New("provedor fora do ar"), false)
	assert.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	require.Eventually(t, func() bool { return fs.callCount() == 1 }, 5*time.Second, 10*time.Millisecond)

	fs.set(nil, true)
	assert.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	require.Eventually(t, func() bool { return fs.callCount() == 2 }, 5*time.Second, 10*time.Millisecond)

	// O processo sobreviveu ao panic: a API segue respondendo e enviando.
	fs.set(nil, false)
	assert.Equal(t, http.StatusAccepted, e.forgot(t, u.Email))
	token := waitToken(t, fs, u.Email, 1)
	code, _ := e.reset(t, token, "senha-nova-456")
	assert.Equal(t, http.StatusNoContent, code)
}
