package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/handlers"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

const inviteBaseURL = "http://front.test"

type inviteEnv struct {
	profileEnv // reaproveita do() e o fakeSender
}

type inviteUser struct {
	Login      string
	FamilyName string
	Token      string
}

// setupInviteEnv sobe Postgres e expõe POST /family/invites com o limite de
// e-mails por usuário informado. billingSvc nil: sem limite de plano nos testes.
func setupInviteEnv(t *testing.T, perUserPerDay int) *inviteEnv {
	t.Helper()
	db, cleanup := testutil.SetupPostgresContainer(t)
	t.Cleanup(cleanup)
	repos := repository.New(db)

	authSvc := auth.NewService("teste-secret-invite", 1)
	sender := &fakeSender{}
	fh := handlers.NewFamilyHandler(repos.Families, repos.Invites, repos.Users, nil, sender, inviteBaseURL,
		middleware.NewInMemoryRateLimiter(perUserPerDay, 24*time.Hour))

	mux := http.NewServeMux()
	mux.Handle("POST /family/invites", middleware.Authenticate(authSvc, repos.Blocklist)(http.HandlerFunc(fh.CreateInvite)))
	return &inviteEnv{profileEnv{mux: mux, repos: repos, authSvc: authSvc, sender: sender}}
}

func (e *inviteEnv) createUser(t *testing.T, familyName string) inviteUser {
	t.Helper()
	ctx := context.Background()
	fam := &models.Family{Name: familyName}
	require.NoError(t, e.repos.Families.Create(ctx, fam))
	suffix := uuid.NewString()[:8]
	user := &models.User{Login: "convidador_" + suffix, Email: "convidador_" + suffix + "@example.com", PasswordHash: "x", FamilyID: fam.ID}
	require.NoError(t, e.repos.Users.Create(ctx, user))
	require.NoError(t, e.repos.Families.AddMember(ctx, fam.ID, user.ID))
	token, err := e.authSvc.GenerateToken(user)
	require.NoError(t, err)
	return inviteUser{Login: user.Login, FamilyName: familyName, Token: token}
}

// invite cria um convite e exige 201 com token; devolve o token do convite.
func (e *inviteEnv) invite(t *testing.T, u inviteUser, target string) string {
	t.Helper()
	body := map[string]string{}
	if target != "" {
		body["target_email"] = target
	}
	code, raw := e.do(t, "POST", "/family/invites", u.Token, body)
	require.Equal(t, http.StatusCreated, code, raw)
	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	require.NotEmpty(t, resp.Token)
	return resp.Token
}

func (e *inviteEnv) waitSent(t *testing.T, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(e.sender.sent()) >= n }, 5*time.Second, 10*time.Millisecond)
}

func newTarget() string { return "convidado_" + uuid.NewString()[:8] + "@example.com" }

func TestFamilyInviteEmailSentToTarget(t *testing.T) {
	e := setupInviteEnv(t, 10)
	u := e.createUser(t, "Família Silva")
	target := newTarget()

	token := e.invite(t, u, target)
	e.waitSent(t, 1)

	msgs := e.sender.sentTo(target)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].Subject, u.Login)
	assert.Contains(t, msgs[0].Subject, "Família Silva")
	assert.Contains(t, msgs[0].HTML, "Família Silva")
	assert.Contains(t, msgs[0].HTML, u.Login)
	assert.Contains(t, msgs[0].HTML, inviteBaseURL+"/convite#token="+token)
}

func TestFamilyInviteEmailNotSentWithoutTarget(t *testing.T) {
	e := setupInviteEnv(t, 10)
	u := e.createUser(t, "Família Sem Alvo")

	e.invite(t, u, "")
	e.assertNoEmail(t)
}

func TestFamilyInviteEmailIsolation(t *testing.T) {
	e := setupInviteEnv(t, 10)
	a := e.createUser(t, "Família Alfa")
	b := e.createUser(t, "Família Beta")
	targetA, targetB := newTarget(), newTarget()

	tokenA := e.invite(t, a, targetA)
	tokenB := e.invite(t, b, targetB)
	e.waitSent(t, 2)

	msgsA, msgsB := e.sender.sentTo(targetA), e.sender.sentTo(targetB)
	require.Len(t, msgsA, 1)
	require.Len(t, msgsB, 1)

	assert.Contains(t, msgsA[0].HTML, tokenA)
	assert.Contains(t, msgsA[0].HTML, "Família Alfa")
	for _, leak := range []string{tokenB, "Família Beta", b.Login} {
		assert.NotContains(t, msgsA[0].HTML, leak)
		assert.NotContains(t, msgsA[0].Subject, leak)
	}
	assert.Contains(t, msgsB[0].HTML, tokenB)
	for _, leak := range []string{tokenA, "Família Alfa", a.Login} {
		assert.NotContains(t, msgsB[0].HTML, leak)
		assert.NotContains(t, msgsB[0].Subject, leak)
	}
}

func TestFamilyInviteEmailEscapesFamilyName(t *testing.T) {
	e := setupInviteEnv(t, 10)
	u := e.createUser(t, "<script>alert(1)</script>")
	target := newTarget()

	e.invite(t, u, target)
	e.waitSent(t, 1)

	msg := e.sender.sentTo(target)[0]
	assert.Contains(t, msg.HTML, "&lt;script&gt;alert(1)&lt;/script&gt;")
	assert.NotContains(t, msg.HTML, "<script>")
}

func TestFamilyInviteEmailRateLimited(t *testing.T) {
	e := setupInviteEnv(t, 10) // mesmo limite de cmd/api/routes.go
	u := e.createUser(t, "Família Limitada")

	for i := 0; i < 10; i++ {
		e.invite(t, u, newTarget())
	}
	e.waitSent(t, 10)

	// 11º convite do dia: ainda 201 com token, mas sem novo e-mail.
	e.invite(t, u, newTarget())
	require.Never(t, func() bool { return e.sender.callCount() > 10 }, neverWindow, 20*time.Millisecond)

	// O limite é por usuário: outro usuário continua recebendo.
	other := e.createUser(t, "Outra Família")
	target := newTarget()
	e.invite(t, other, target)
	require.Eventually(t, func() bool { return len(e.sender.sentTo(target)) == 1 }, 5*time.Second, 10*time.Millisecond)
}

func TestFamilyInviteEmailSenderFailureStill201(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		panics bool
	}{
		{"erro", errors.New("provedor fora do ar"), false},
		{"panic", nil, true},
	}
	e := setupInviteEnv(t, 10)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.sender.set(tc.err, tc.panics)
			before := e.sender.callCount()
			u := e.createUser(t, "Família Falha")

			e.invite(t, u, newTarget()) // exige 201 com token
			// O envio foi tentado (e falhou) sem derrubar o processo.
			require.Eventually(t, func() bool { return e.sender.callCount() == before+1 }, 5*time.Second, 10*time.Millisecond)
		})
	}
}
