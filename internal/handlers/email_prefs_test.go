package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/willGabrielPereira/finager-backend/internal/handlers"
	"github.com/willGabrielPereira/finager-backend/internal/mailer"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

var unsubTestSecret = []byte("segredo-descadastro")

type unsubEnv struct {
	srv   *httptest.Server
	db    *pgxpool.Pool
	repos *repository.Container
}

func setupUnsubEnv(t *testing.T) *unsubEnv {
	t.Helper()
	db, cleanup := testutil.SetupPostgresContainer(t)
	t.Cleanup(cleanup)
	repos := repository.New(db)

	h := handlers.NewEmailPrefsHandler(repos.Users, unsubTestSecret)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /email/unsubscribe", h.Unsubscribe)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &unsubEnv{srv: srv, db: db, repos: repos}
}

// user cria um usuário numa família própria (famílias distintas entre chamadas).
func (e *unsubEnv) user(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	fam := &models.Family{Name: "Família Unsub " + uuid.NewString()[:8]}
	require.NoError(t, e.repos.Families.Create(ctx, fam))
	u := &models.User{Login: "unsub_" + uuid.NewString()[:8], Email: "unsub_" + uuid.NewString()[:8] + "@example.com", PasswordHash: "x", FamilyID: fam.ID}
	require.NoError(t, e.repos.Users.Create(ctx, u))
	require.NoError(t, e.repos.Families.AddMember(ctx, fam.ID, u.ID))
	return u.ID
}

func (e *unsubEnv) optedOut(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var v bool
	require.NoError(t, e.db.QueryRow(context.Background(), `SELECT email_reminders_opt_out FROM users WHERE id = $1`, id).Scan(&v))
	return v
}

func (e *unsubEnv) unsubscribe(t *testing.T, u, s string) int {
	t.Helper()
	b, err := json.Marshal(map[string]string{"u": u, "s": s})
	require.NoError(t, err)
	resp, err := http.Post(e.srv.URL+"/email/unsubscribe", "application/json", bytes.NewReader(b))
	require.NoError(t, err)
	resp.Body.Close()
	return resp.StatusCode
}

func TestUnsubscribeValidSignature(t *testing.T) {
	e := setupUnsubEnv(t)
	a := e.user(t) // família A
	b := e.user(t) // família B

	code := e.unsubscribe(t, a.String(), mailer.UnsubscribeSig(unsubTestSecret, a))
	assert.Equal(t, http.StatusNoContent, code)
	assert.True(t, e.optedOut(t, a))
	assert.False(t, e.optedOut(t, b), "descadastro de A nunca altera B")

	// GET não muta estado: a rota só aceita POST.
	resp, err := http.Get(e.srv.URL + "/email/unsubscribe?u=" + b.String() + "&s=" + mailer.UnsubscribeSig(unsubTestSecret, b))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.False(t, e.optedOut(t, b))
}

func TestUnsubscribeInvalidSignature(t *testing.T) {
	e := setupUnsubEnv(t)
	a := e.user(t)
	b := e.user(t)
	sigA := mailer.UnsubscribeSig(unsubTestSecret, a)

	// Assinatura de A usada para B (outra família).
	assert.Equal(t, http.StatusBadRequest, e.unsubscribe(t, b.String(), sigA))
	// Assinatura adulterada.
	tampered := []byte(sigA)
	if tampered[0] == 'a' {
		tampered[0] = 'b'
	} else {
		tampered[0] = 'a'
	}
	assert.Equal(t, http.StatusBadRequest, e.unsubscribe(t, a.String(), string(tampered)))
	// Assinatura com outro segredo, vazia, e usuário malformado.
	assert.Equal(t, http.StatusBadRequest, e.unsubscribe(t, a.String(), mailer.UnsubscribeSig([]byte("outro"), a)))
	assert.Equal(t, http.StatusBadRequest, e.unsubscribe(t, a.String(), ""))
	assert.Equal(t, http.StatusBadRequest, e.unsubscribe(t, "nao-e-uuid", sigA))
	// Corpo malformado.
	resp, err := http.Post(e.srv.URL+"/email/unsubscribe", "application/json", strings.NewReader("{"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	assert.False(t, e.optedOut(t, a), "nada muda com assinatura inválida")
	assert.False(t, e.optedOut(t, b), "nada muda com assinatura inválida")
}

func TestUnsubscribeIdempotent(t *testing.T) {
	e := setupUnsubEnv(t)
	a := e.user(t)
	sig := mailer.UnsubscribeSig(unsubTestSecret, a)

	assert.Equal(t, http.StatusNoContent, e.unsubscribe(t, a.String(), sig))
	assert.Equal(t, http.StatusNoContent, e.unsubscribe(t, a.String(), sig))
	assert.True(t, e.optedOut(t, a))
}
