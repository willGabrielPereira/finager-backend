package jobs_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/willGabrielPereira/finager-backend/internal/jobs"
	"github.com/willGabrielPereira/finager-backend/internal/mailer"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

var testSecret = []byte("segredo-descadastro-teste")

// dailyCap alto: testes com vários envios não colidem com o teto diário medido no banco.
const highDailyCap = 100

// fakeSender guarda as mensagens; onSend (opcional) roda antes de registrar e pode dar panic.
type fakeSender struct {
	mu     sync.Mutex
	msgs   []mailer.Message
	calls  int
	onSend func(call int)
}

func (f *fakeSender) Send(_ context.Context, m mailer.Message) error {
	f.mu.Lock()
	f.calls++
	call, hook := f.calls, f.onSend
	f.mu.Unlock()
	if hook != nil {
		hook(call)
	}
	f.mu.Lock()
	f.msgs = append(f.msgs, m)
	f.mu.Unlock()
	return nil
}

func (f *fakeSender) sent() []mailer.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]mailer.Message(nil), f.msgs...)
}

type env struct {
	db    *pgxpool.Pool
	repos *repository.Container
}

func setup(t *testing.T) *env {
	t.Helper()
	db, cleanup := testutil.SetupPostgresContainer(t)
	t.Cleanup(cleanup)
	repos := repository.New(db)
	return &env{db: db, repos: repos}
}

func (e *env) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := e.db.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

// family cria uma família com created_at = now() - age.
func (e *env) family(t *testing.T, name string, age time.Duration) uuid.UUID {
	t.Helper()
	f := &models.Family{Name: name}
	require.NoError(t, e.repos.Families.Create(context.Background(), f))
	e.exec(t, `UPDATE families SET created_at = now() - make_interval(secs => $1) WHERE id = $2`, age.Seconds(), f.ID)
	return f.ID
}

type member struct {
	ID    uuid.UUID
	Login string
	Email string
}

// member cria um usuário com users.family_id = famID e linha em family_members.
// email "" grava NULL.
func (e *env) member(t *testing.T, famID uuid.UUID, email string) member {
	t.Helper()
	u := &models.User{Login: "u_" + uuid.NewString()[:8], Email: email, PasswordHash: "x", FamilyID: famID}
	require.NoError(t, e.repos.Users.Create(context.Background(), u))
	require.NoError(t, e.repos.Families.AddMember(context.Background(), famID, u.ID))
	return member{ID: u.ID, Login: u.Login, Email: email}
}

func newEmail() string { return "m_" + uuid.NewString()[:8] + "@example.com" }

// ofxImport grava uma transação com imported_at = now() - age (source OFX ou MANUAL).
func (e *env) ofxImport(t *testing.T, famID, userID uuid.UUID, fitid, source string, age time.Duration) {
	t.Helper()
	var accID uuid.UUID
	require.NoError(t, e.db.QueryRow(context.Background(),
		`INSERT INTO accounts (name, institution, family_id, created_by) VALUES ('Conta', 'Banco', $1, $2) RETURNING id`,
		famID, userID).Scan(&accID))
	e.exec(t, `INSERT INTO transactions (fitid, type, date_posted, amount, account_id, family_id, created_by, imported_at, source)
		VALUES ($1, 'DEBIT', now(), 10, $2, $3, $4, now() - make_interval(secs => $5), $6)`,
		fitid, accID, famID, userID, age.Seconds(), source)
}

func (e *env) run(t *testing.T, fs *fakeSender, dailyCap int) int {
	t.Helper()
	sent, err := jobs.RunOFXReminders(context.Background(), e.repos.Families, e.repos.Users, fs, "http://front.test/", testSecret, dailyCap, 0)
	require.NoError(t, err)
	return sent
}

const day = 24 * time.Hour

var unsubRe = regexp.MustCompile(`/descadastrar\?u=([0-9a-f-]{36})&(?:amp;)?s=([0-9a-f]+)`)

// unsubLinks extrai (userID -> assinatura) dos links de descadastro do HTML.
func unsubLinks(t *testing.T, html string) map[uuid.UUID]string {
	t.Helper()
	out := map[uuid.UUID]string{}
	for _, m := range unsubRe.FindAllStringSubmatch(html, -1) {
		id, err := uuid.Parse(m[1])
		require.NoError(t, err)
		out[id] = m[2]
	}
	return out
}

func TestOFXReminderStaleFamily(t *testing.T) {
	e := setup(t)
	fam := e.family(t, "Família Silva", 31*day)
	a := e.member(t, fam, newEmail())
	b := e.member(t, fam, newEmail())

	fs := &fakeSender{}
	assert.Equal(t, 1, e.run(t, fs, highDailyCap))

	msgs := fs.sent()
	require.Len(t, msgs, 1, "exatamente 1 envio por família")
	assert.ElementsMatch(t, []string{a.Email, b.Email}, msgs[0].To)
	assert.Contains(t, msgs[0].Subject, "Família Silva")

	links := unsubLinks(t, msgs[0].HTML)
	require.Len(t, links, 2, "1 link de descadastro por destinatário, no mesmo e-mail")
	for _, m := range []member{a, b} {
		sig, ok := links[m.ID]
		require.True(t, ok, "link de %s ausente", m.Login)
		assert.True(t, mailer.VerifyUnsubscribe(testSecret, m.ID, sig))
		assert.Contains(t, msgs[0].HTML, m.Login)
	}
	assert.Contains(t, msgs[0].HTML, "http://front.test/descadastrar?u=")
}

func TestOFXReminderFreshFamilies(t *testing.T) {
	e := setup(t)
	// Criada agora, sem importações.
	f1 := e.family(t, "Nova", time.Hour)
	e.member(t, f1, newEmail())
	// Antiga, mas com importação OFX há 5 dias.
	f2 := e.family(t, "Antiga Ativa", 90*day)
	u := e.member(t, f2, newEmail())
	e.ofxImport(t, f2, u.ID, "F1", "OFX", 5*day)

	fs := &fakeSender{}
	assert.Equal(t, 0, e.run(t, fs, highDailyCap))
	assert.Empty(t, fs.sent())
}

func TestOFXReminderIdempotent(t *testing.T) {
	e := setup(t)
	fam := e.family(t, "Idem", 31*day)
	e.member(t, fam, newEmail())

	fs := &fakeSender{}
	assert.Equal(t, 1, e.run(t, fs, highDailyCap))
	assert.Equal(t, 0, e.run(t, fs, highDailyCap))
	assert.Equal(t, 0, e.run(t, fs, highDailyCap))
	assert.Len(t, fs.sent(), 1)
}

func TestOFXReminderAfterNewImport(t *testing.T) {
	e := setup(t)
	fam := e.family(t, "Reimporta", 90*day)
	u := e.member(t, fam, newEmail())

	fs := &fakeSender{}
	require.Equal(t, 1, e.run(t, fs, highDailyCap))
	require.Equal(t, 0, e.run(t, fs, highDailyCap))

	// Lembrete enviado há 60 dias; depois disso a família importou (há 31 dias) e parou de novo.
	e.exec(t, `UPDATE families SET ofx_reminder_sent_at = now() - interval '60 days' WHERE id = $1`, fam)
	e.ofxImport(t, fam, u.ID, "NOVO", "OFX", 31*day)

	assert.Equal(t, 1, e.run(t, fs, highDailyCap), "nova importação reabre o lembrete")
	assert.Len(t, fs.sent(), 2)
	assert.Equal(t, 0, e.run(t, fs, highDailyCap))
}

func TestOFXReminderIgnoresManual(t *testing.T) {
	e := setup(t)
	fam := e.family(t, "Manual", 31*day)
	u := e.member(t, fam, newEmail())
	// Transação manual recente não conta como importação OFX.
	e.ofxImport(t, fam, u.ID, "MANUAL_1", "MANUAL", 0)

	fs := &fakeSender{}
	assert.Equal(t, 1, e.run(t, fs, highDailyCap))
}

func TestOFXReminderReimportSkippedDoesNotReset(t *testing.T) {
	e := setup(t)
	fam := e.family(t, "Duplicado", 90*day)
	u := e.member(t, fam, newEmail())
	e.ofxImport(t, fam, u.ID, "FIT-1", "OFX", 31*day)

	var accID uuid.UUID
	require.NoError(t, e.db.QueryRow(context.Background(), `SELECT account_id FROM transactions WHERE fitid = 'FIT-1'`).Scan(&accID))

	// Re-importar o mesmo FITID pelo caminho real de import é ignorado (skipped)
	// e não atualiza imported_at — a família continua elegível.
	res, err := e.repos.Transactions.BulkUpsert(context.Background(), []models.Transaction{{
		FITID: "FIT-1", Type: "DEBIT", DatePosted: time.Now(), Amount: 10, Name: "", AccountID: accID, FamilyID: fam, CreatedBy: u.ID,
	}})
	require.NoError(t, err)
	require.Equal(t, 1, res.Skipped)

	fs := &fakeSender{}
	assert.Equal(t, 1, e.run(t, fs, highDailyCap))
}

func TestOFXReminderConcurrentRuns(t *testing.T) {
	e := setup(t)
	const families = 4
	for i := range families {
		fam := e.family(t, "Conc "+string(rune('A'+i)), 31*day)
		e.member(t, fam, newEmail())
		e.member(t, fam, newEmail()) // 2 destinatários por família: envios != destinatários
	}

	fs := &fakeSender{}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := jobs.RunOFXReminders(context.Background(), e.repos.Families, e.repos.Users, fs, "http://front.test", testSecret, highDailyCap, 0)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	msgs := fs.sent()
	assert.Len(t, msgs, families, "total de envios = famílias elegíveis, não destinatários")
	seen := map[string]bool{}
	for _, m := range msgs {
		assert.Len(t, m.To, 2)
		assert.False(t, seen[m.To[0]], "família enviada duas vezes")
		seen[m.To[0]] = true
	}
}

func TestOFXReminderRecipients(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	fam := e.family(t, "Destinatários", 31*day)

	ok := e.member(t, fam, newEmail())

	optOut := e.member(t, fam, newEmail())
	e.exec(t, `UPDATE users SET email_reminders_opt_out = true WHERE id = $1`, optOut.ID)

	noEmail := e.member(t, fam, "")

	admin := e.member(t, fam, newEmail())
	e.exec(t, `UPDATE users SET role = 'admin' WHERE id = $1`, admin.ID)
	mod := e.member(t, fam, newEmail())
	e.exec(t, `UPDATE users SET role = 'moderator' WHERE id = $1`, mod.ID)

	// Migrado via Join: linha em family_members(F) continua, mas users.family_id = G.
	other := e.family(t, "Outra", time.Hour)
	migrated := e.member(t, fam, newEmail())
	require.NoError(t, e.repos.Families.AddMember(ctx, other, migrated.ID))
	require.NoError(t, e.repos.Users.UpdateFamilyID(ctx, migrated.ID, other))

	// CASO CRÍTICO (PII): removido via RemoveMember — users.family_id continua = F.
	removed := e.member(t, fam, newEmail())
	require.NoError(t, e.repos.Families.RemoveMember(ctx, fam, removed.ID))

	// Dormente sem sessão: excluído.
	dormant := e.member(t, fam, newEmail())
	e.exec(t, `UPDATE users SET last_login_at = now() - interval '200 days', created_at = now() - interval '300 days' WHERE id = $1`, dormant.ID)

	// CASO CRÍTICO (engajamento): dormente por last_login_at, mas com refresh token válido -> incluído.
	refreshOnly := e.member(t, fam, newEmail())
	e.exec(t, `UPDATE users SET last_login_at = now() - interval '200 days', created_at = now() - interval '300 days' WHERE id = $1`, refreshOnly.ID)
	require.NoError(t, e.repos.RefreshTokens.Create(ctx, &models.RefreshToken{UserID: refreshOnly.ID, TokenHash: uuid.NewString(), ExpiresAt: time.Now().Add(7 * day)}))

	// Dormente com refresh revogado ou expirado: continua excluído.
	revoked := e.member(t, fam, newEmail())
	e.exec(t, `UPDATE users SET last_login_at = now() - interval '200 days' WHERE id = $1`, revoked.ID)
	require.NoError(t, e.repos.RefreshTokens.Create(ctx, &models.RefreshToken{UserID: revoked.ID, TokenHash: uuid.NewString(), ExpiresAt: time.Now().Add(7 * day), Revoked: true}))
	expired := e.member(t, fam, newEmail())
	e.exec(t, `UPDATE users SET last_login_at = now() - interval '200 days' WHERE id = $1`, expired.ID)
	require.NoError(t, e.repos.RefreshTokens.Create(ctx, &models.RefreshToken{UserID: expired.ID, TokenHash: uuid.NewString(), ExpiresAt: time.Now().Add(-time.Hour)}))

	// E-mail legado inválido: descartado individualmente, sem derrubar a família.
	invalid := e.member(t, fam, "endereco invalido sem arroba")

	// Família sem nenhum elegível -> 0 envios.
	famNone := e.family(t, "Sem Elegíveis", 31*day)
	none := e.member(t, famNone, newEmail())
	e.exec(t, `UPDATE users SET email_reminders_opt_out = true WHERE id = $1`, none.ID)
	// Família só com e-mail inválido -> pulada sem enviar.
	famInvalid := e.family(t, "Só Inválido", 31*day)
	e.member(t, famInvalid, "invalido@@x")

	fs := &fakeSender{}
	assert.Equal(t, 1, e.run(t, fs, highDailyCap))

	msgs := fs.sent()
	require.Len(t, msgs, 1)
	assert.ElementsMatch(t, []string{ok.Email, refreshOnly.Email}, msgs[0].To)

	links := unsubLinks(t, msgs[0].HTML)
	assert.Len(t, links, 2)
	assert.Contains(t, links, ok.ID)
	assert.Contains(t, links, refreshOnly.ID)

	for _, x := range []member{optOut, noEmail, admin, mod, migrated, removed, dormant, revoked, expired, invalid} {
		assert.NotContains(t, links, x.ID, "%s não pode ter link", x.Login)
		assert.NotContains(t, msgs[0].HTML, x.Login)
		if x.Email != "" {
			assert.NotContains(t, msgs[0].To, x.Email)
			assert.NotContains(t, msgs[0].HTML, x.Email)
		}
	}
	// PII: o ex-membro removido não aparece em lugar nenhum do e-mail.
	assert.NotContains(t, msgs[0].HTML, removed.ID.String())
}

func TestOFXReminderIsolation(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	famA := e.family(t, "Família Alfa", 31*day)
	a1, a2 := e.member(t, famA, newEmail()), e.member(t, famA, newEmail())
	exA := e.member(t, famA, newEmail())
	require.NoError(t, e.repos.Families.RemoveMember(ctx, famA, exA.ID))

	famB := e.family(t, "Família Beta", 32*day)
	b1, b2 := e.member(t, famB, newEmail()), e.member(t, famB, newEmail())

	fs := &fakeSender{}
	require.Equal(t, 2, e.run(t, fs, highDailyCap))

	var msgA, msgB *mailer.Message
	for _, m := range fs.sent() {
		switch {
		case strings.Contains(m.Subject, "Alfa"):
			msgA = &m
		case strings.Contains(m.Subject, "Beta"):
			msgB = &m
		}
	}
	require.NotNil(t, msgA)
	require.NotNil(t, msgB)

	check := func(m *mailer.Message, own []member, ownName string, foreign []member, foreignName string) {
		emails := make([]string, len(own))
		ids := make([]uuid.UUID, len(own))
		for i, o := range own {
			emails[i], ids[i] = o.Email, o.ID
		}
		assert.ElementsMatch(t, emails, m.To)
		links := unsubLinks(t, m.HTML)
		assert.Len(t, links, len(own))
		for _, id := range ids {
			assert.Contains(t, links, id)
		}
		assert.Contains(t, m.HTML, ownName)
		assert.NotContains(t, m.HTML, foreignName)
		assert.NotContains(t, m.Subject, foreignName)
		for _, f := range foreign {
			assert.NotContains(t, m.To, f.Email)
			assert.NotContains(t, m.HTML, f.Email)
			assert.NotContains(t, m.HTML, f.Login)
			assert.NotContains(t, m.HTML, f.ID.String())
		}
	}
	// Ex-membro removido de A: nem no To nem nos links de A (nem de B).
	check(msgA, []member{a1, a2}, "Família Alfa", []member{b1, b2, exA}, "Família Beta")
	check(msgB, []member{b1, b2}, "Família Beta", []member{a1, a2, exA}, "Família Alfa")
}

func (e *env) staleFamilies(t *testing.T, n int) {
	t.Helper()
	for i := range n {
		fam := e.family(t, "Cota "+string(rune('A'+i)), time.Duration(31+i)*day)
		e.member(t, fam, newEmail())
	}
}

func TestOFXReminderDailyCapEnforced(t *testing.T) {
	e := setup(t)
	e.staleFamilies(t, 5)

	fs := &fakeSender{}
	assert.Equal(t, 2, e.run(t, fs, 2))
	assert.Len(t, fs.sent(), 2)

	var marked int
	require.NoError(t, e.db.QueryRow(context.Background(), `SELECT count(*) FROM families WHERE ofx_reminder_sent_at IS NOT NULL`).Scan(&marked))
	assert.Equal(t, 2, marked)
}

func TestOFXReminderDailyCapAcrossRuns(t *testing.T) {
	e := setup(t)
	e.staleFamilies(t, 5)
	fs := &fakeSender{}

	assert.Equal(t, 2, e.run(t, fs, 2), "1ª chamada")
	// 2ª chamada imediata (reinício / outra réplica): teto medido no banco já esgotado.
	assert.Equal(t, 0, e.run(t, fs, 2), "2ª chamada imediata deve receber 0 famílias")
	assert.Len(t, fs.sent(), 2)

	// Os 2 envios saem da janela de 20h.
	e.exec(t, `UPDATE families SET ofx_reminder_sent_at = now() - interval '21 hours' WHERE ofx_reminder_sent_at IS NOT NULL`)
	assert.Equal(t, 2, e.run(t, fs, 2), "3ª chamada após a janela")
	assert.Len(t, fs.sent(), 4)
}

func TestNextDailyRun(t *testing.T) {
	sp, err := time.LoadLocation("America/Sao_Paulo")
	require.NoError(t, err)
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, sp) }

	cases := []struct {
		name      string
		now, want time.Time
	}{
		{"antes das 19h", at(2026, time.March, 10, 8, 30), at(2026, time.March, 10, 19, 0)},
		{"depois das 19h", at(2026, time.March, 10, 21, 15), at(2026, time.March, 11, 19, 0)},
		{"exatamente 19h", at(2026, time.March, 10, 19, 0), at(2026, time.March, 11, 19, 0)},
		{"virada de ano", at(2026, time.December, 31, 23, 0), at(2027, time.January, 1, 19, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := jobs.NextDailyRun(c.now)
			assert.True(t, got.Equal(c.want), "got %s, want %s", got, c.want)
			// Mesmo instante expresso em UTC dá o mesmo resultado: independe do fuso da máquina.
			assert.True(t, jobs.NextDailyRun(c.now.UTC()).Equal(c.want))
		})
	}
}

func TestOFXReminderStopsOnCancel(t *testing.T) {
	e := setup(t)
	e.staleFamilies(t, 3)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fs := &fakeSender{onSend: func(int) { cancel() }}

	start := time.Now()
	sent, err := jobs.RunOFXReminders(ctx, e.repos.Families, e.repos.Users, fs, "http://front.test", testSecret, highDailyCap, time.Hour)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, sent)
	assert.Len(t, fs.sent(), 1)
	assert.Less(t, time.Since(start), 10*time.Second, "pausa deve ser cancelável")
}

func TestOFXReminderJobContinuesAfterFamilyPanic(t *testing.T) {
	e := setup(t)
	// ORDER BY last_ref DESC: a família mais recente (31d) é processada primeiro.
	first := e.family(t, "Primeira", 31*day)
	e.member(t, first, newEmail())
	second := e.family(t, "Segunda", 40*day)
	s := e.member(t, second, newEmail())

	fs := &fakeSender{onSend: func(call int) {
		if call == 1 {
			panic(errors.New("panic proposital no envio"))
		}
	}}

	var sent int
	require.NotPanics(t, func() { sent = e.run(t, fs, highDailyCap) })
	assert.Equal(t, 1, sent)
	msgs := fs.sent()
	require.Len(t, msgs, 1)
	assert.Equal(t, []string{s.Email}, msgs[0].To, "a 2ª família recebe o seu envio")
}
