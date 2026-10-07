package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/willGabrielPereira/finager-backend/internal/auth"
	"github.com/willGabrielPereira/finager-backend/internal/handlers"
	"github.com/willGabrielPereira/finager-backend/internal/middleware"
	"github.com/willGabrielPereira/finager-backend/internal/models"
	"github.com/willGabrielPereira/finager-backend/internal/repository"
	"github.com/willGabrielPereira/finager-backend/internal/testutil"
)

// newSupportMux replica as rotas de concessão (/family/support-access) e de dump
// (/admin/families/{id}/dump) com os middlewares reais de cmd/api/routes.go.
func newSupportMux(repos *repository.Container, authSvc *auth.Service) http.Handler {
	authMid := middleware.Authenticate(authSvc, repos.Blocklist)
	adminOrModMid := middleware.RequireRole(repos.Users, "admin", "moderator")
	elevatedMid := middleware.RequireElevated(authSvc, repos.Blocklist)

	adminHandler := handlers.NewAdminHandler(repos.Users, repos.Coupons, repos.SignupInvites, repos.Families, repos.Audit, repos.Dumps, nil)
	familyHandler := handlers.NewFamilyHandler(repos.Families, repos.Invites, repos.Users, nil, nil, "", nil)

	mux := http.NewServeMux()
	mux.Handle("GET /family/support-access", authMid(http.HandlerFunc(familyHandler.GetSupportAccess)))
	mux.Handle("PUT /family/support-access", authMid(http.HandlerFunc(familyHandler.SetSupportAccess)))
	mux.Handle("GET /admin/families/{id}/dump", authMid(adminOrModMid(elevatedMid(http.HandlerFunc(adminHandler.FamilyDump)))))
	return mux
}

func userToken(t *testing.T, authSvc *auth.Service, u *models.User) string {
	t.Helper()
	tok, err := authSvc.GenerateToken(u)
	require.NoError(t, err)
	return tok
}

// seedFamilyData insere conta, tag e transação direto no banco e devolve o nome único da transação.
func seedFamilyData(t *testing.T, ctx context.Context, db *pgxpool.Pool, u *models.User, txName string) {
	t.Helper()
	var accID, tagID, txID uuid.UUID
	require.NoError(t, db.QueryRow(ctx,
		`INSERT INTO accounts (name, institution, family_id, created_by) VALUES ('Conta', 'Banco', $1, $2) RETURNING id`,
		u.FamilyID, u.ID).Scan(&accID))
	require.NoError(t, db.QueryRow(ctx,
		`INSERT INTO tags (name, family_id) VALUES ($1, $2) RETURNING id`, "tag-"+txName, u.FamilyID).Scan(&tagID))
	require.NoError(t, db.QueryRow(ctx,
		`INSERT INTO transactions (fitid, type, date_posted, amount, name, memo, account_id, family_id, created_by)
		 VALUES ($1, 'DEBIT', now(), 10.50, $2, 'memo com	tab', $3, $4, $5) RETURNING id`,
		uuid.NewString(), txName, accID, u.FamilyID, u.ID).Scan(&txID))
	_, err := db.Exec(ctx, `INSERT INTO transaction_tags (transaction_id, tag_id) VALUES ($1, $2)`, txID, tagID)
	require.NoError(t, err)
}

func TestSupportDump(t *testing.T) {
	db, cleanup := testutil.SetupPostgresContainer(t)
	defer cleanup()
	ctx := context.Background()
	repos := repository.New(db)
	authSvc := auth.NewService("teste-secret-support", 1)
	mux := newSupportMux(repos, authSvc)

	famUser := createTestUser(t, ctx, repos, "user")
	otherUser := createTestUser(t, ctx, repos, "user")
	admin := createTestUser(t, ctx, repos, "admin")
	moderator := createTestUser(t, ctx, repos, "moderator")
	_, err := db.Exec(ctx, `INSERT INTO classifier_states (family_id, total_docs, vocabulary) VALUES (NULL, 7, '["GLOBALWORD"]')`)
	require.NoError(t, err)
	seedFamilyData(t, ctx, db, famUser, "PADARIA-FAMILIA-A")
	seedFamilyData(t, ctx, db, otherUser, "MERCADO-FAMILIA-B")

	dumpPath := "/admin/families/" + famUser.FamilyID.String() + "/dump?reason=ticket-123"
	get := func(u *models.User, path string, elevated bool) *httptest.ResponseRecorder {
		el := ""
		if elevated {
			el = generateElevatedToken(t, authSvc, u)
		}
		return adminRequestElevated(t, mux, "GET", path, userToken(t, authSvc, u), el, nil)
	}

	t.Run("sem concessão → 403", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, get(admin, dumpPath, true).Code)
	})

	// A própria família concede; o toggle só afeta a família do token.
	rr := adminRequest(t, mux, "PUT", "/family/support-access", userToken(t, authSvc, famUser), map[string]any{"enabled": true, "days": 3})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	rr = adminRequest(t, mux, "GET", "/family/support-access", userToken(t, authSvc, otherUser), nil)
	assert.Contains(t, rr.Body.String(), `"enabled":false`)

	t.Run("validações e proteções", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, get(admin, "/admin/families/"+famUser.FamilyID.String()+"/dump", true).Code, "sem reason")
		assert.Equal(t, http.StatusForbidden, get(admin, dumpPath, false).Code, "sem elevação")
		assert.Equal(t, http.StatusForbidden, get(famUser, dumpPath, true).Code, "role user")
		assert.Equal(t, http.StatusBadRequest, adminRequest(t, mux, "PUT", "/family/support-access", userToken(t, authSvc, famUser), map[string]any{"enabled": true, "days": 99}).Code)
	})

	t.Run("concessão de outra família não vale", func(t *testing.T) {
		other := "/admin/families/" + otherUser.FamilyID.String() + "/dump?reason=x"
		assert.Equal(t, http.StatusForbidden, get(admin, other, true).Code)
	})

	var dump string
	for _, u := range []*models.User{admin, moderator} {
		rr := get(u, dumpPath, true)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		dump = rr.Body.String()
		assert.Contains(t, dump, "CREATE TABLE IF NOT EXISTS transactions")
		assert.Contains(t, dump, "COPY transactions (")
		assert.True(t, strings.HasSuffix(strings.TrimSpace(dump), repository.DumpEndMarker))
	}

	t.Run("conteúdo: isolamento e segredos", func(t *testing.T) {
		assert.Contains(t, dump, "PADARIA-FAMILIA-A")
		assert.NotContains(t, dump, "MERCADO-FAMILIA-B")
		assert.NotContains(t, dump, "hash-nao-usado-neste-teste")
		assert.NotContains(t, dump, otherUser.Email)
		assert.Contains(t, dump, "REDACTED")
		assert.Contains(t, dump, "GLOBALWORD", "estado global do classificador (family_id NULL)")
	})

	t.Run("auditoria gravada", func(t *testing.T) {
		var n int
		require.NoError(t, db.QueryRow(ctx,
			`SELECT count(*) FROM admin_audit_log WHERE family_id = $1 AND reason = 'ticket-123'`, famUser.FamilyID).Scan(&n))
		assert.Equal(t, 2, n)
	})

	t.Run("concessão expirada → 403", func(t *testing.T) {
		past := time.Now().Add(-time.Minute)
		require.NoError(t, repos.Families.SetSupportAccess(ctx, famUser.FamilyID, &past))
		assert.Equal(t, http.StatusForbidden, get(admin, dumpPath, true).Code)
	})

	t.Run("round-trip: dump restaura em banco limpo", func(t *testing.T) {
		db2, cleanup2 := testutil.SetupPostgresContainer(t)
		defer cleanup2()
		restoreDump(t, ctx, db2, dump)

		var txs, users int
		require.NoError(t, db2.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE name = 'PADARIA-FAMILIA-A'`).Scan(&txs))
		require.NoError(t, db2.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&users))
		assert.Equal(t, 1, txs)
		assert.Equal(t, 1, users)
		var globals int
		require.NoError(t, db2.QueryRow(ctx, `SELECT count(*) FROM classifier_states WHERE family_id IS NULL`).Scan(&globals))
		assert.Equal(t, 1, globals)
	})
}

// restoreDump executa o script como o psql faria: DDL via Exec e cada bloco
// "COPY ... FROM stdin;" ... "\." via CopyFrom.
func restoreDump(t *testing.T, ctx context.Context, db *pgxpool.Pool, dump string) {
	t.Helper()
	conn, err := db.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	pg := conn.Conn().PgConn()

	var ddl strings.Builder
	lines := strings.Split(dump, "\n")
	flush := func() {
		if strings.TrimSpace(ddl.String()) == "" {
			return
		}
		_, err := conn.Exec(ctx, ddl.String())
		require.NoError(t, err)
		ddl.Reset()
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "\\set ") || strings.HasPrefix(line, "--"):
		case strings.HasPrefix(line, "COPY ") && strings.HasSuffix(line, "FROM stdin;"):
			flush()
			var data strings.Builder
			for i++; lines[i] != "\\."; i++ {
				data.WriteString(lines[i] + "\n")
			}
			_, err := pg.CopyFrom(ctx, strings.NewReader(data.String()), strings.TrimSuffix(line, "stdin;")+"STDIN")
			require.NoError(t, err, line)
		default:
			ddl.WriteString(line + "\n")
		}
	}
	flush()
}
