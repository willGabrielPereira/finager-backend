package repository

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willGabrielPereira/finager-backend/internal/database"
)

// DumpRepository gera dumps SQL (DDL + COPY) dos dados de uma família.
type DumpRepository struct {
	pool *pgxpool.Pool
}

// NewDumpRepository instancia o repositório de dump.
func NewDumpRepository(pool *pgxpool.Pool) *DumpRepository {
	return &DumpRepository{pool: pool}
}

// DumpEndMarker é a última linha de um dump completo; a CLI a usa para detectar truncamento.
const DumpEndMarker = "-- finager dump completo"

type dumpTable struct {
	name   string
	cols   string // colunas do COPY ... FROM stdin (ordem do SELECT)
	sel    string // expressões do SELECT, na mesma ordem de cols
	filter string // FROM/WHERE; %[1]s = UUID da família (já validado, COPY não aceita parâmetros)
}

// Segredos nunca saem: password_hash vira 'REDACTED' (a coluna é NOT NULL no DDL) e
// external_subscription_id vira NULL. Usuários que só aparecem como autores de contas/transações
// (ex-membros) têm login/e-mail anonimizados e entram como da família para satisfazer as FKs.
const (
	dumpUsersFilter = `FROM users u WHERE u.family_id = '%[1]s'
		OR u.id IN (SELECT user_id FROM family_members WHERE family_id = '%[1]s')
		OR u.id IN (SELECT created_by FROM accounts WHERE family_id = '%[1]s')
		OR u.id IN (SELECT created_by FROM transactions WHERE family_id = '%[1]s')`
	dumpUserIDs = `SELECT u.id ` + dumpUsersFilter
	dumpTagIDs  = `SELECT id FROM tags WHERE family_id = '%[1]s' OR family_id IS NULL`
)

var dumpTables = []dumpTable{
	{"families",
		"id, name, plan, subscription_status, subscription_expires_at, subscription_provider, external_subscription_id, ofx_reminder_sent_at, support_access_until, created_at, updated_at",
		"id, name, plan, subscription_status, subscription_expires_at, subscription_provider, NULL, ofx_reminder_sent_at, support_access_until, created_at, updated_at",
		"FROM families WHERE id = '%[1]s'"},
	{"users",
		"id, login, email, password_hash, family_id, onboarding_completed, onboarding_step, role, last_login_at, email_reminders_opt_out, created_at, updated_at",
		"u.id, CASE WHEN u.family_id = '%[1]s' THEN u.login ELSE 'ex-membro-' || u.id END, CASE WHEN u.family_id = '%[1]s' THEN u.email END, 'REDACTED', '%[1]s'::uuid, u.onboarding_completed, u.onboarding_step, u.role, u.last_login_at, u.email_reminders_opt_out, u.created_at, u.updated_at",
		dumpUsersFilter},
	{"family_members", "family_id, user_id", "family_id, user_id",
		"FROM family_members WHERE family_id = '%[1]s' AND user_id IN (" + dumpUserIDs + ")"},
	{"tags", "id, name, color, icon, family_id, is_system, created_at", "id, name, color, icon, family_id, is_system, created_at",
		"FROM tags WHERE family_id = '%[1]s' OR family_id IS NULL"},
	{"accounts", "id, name, institution, type, family_id, created_by, created_at, updated_at", "id, name, institution, type, family_id, created_by, created_at, updated_at",
		"FROM accounts WHERE family_id = '%[1]s'"},
	{"account_allowed_users", "account_id, user_id", "account_id, user_id",
		"FROM account_allowed_users WHERE account_id IN (SELECT id FROM accounts WHERE family_id = '%[1]s') AND user_id IN (" + dumpUserIDs + ")"},
	{"transactions",
		"id, fitid, type, date_posted, amount, name, memo, account_id, family_id, created_by, imported_at, manually_tagged, status, is_transfer, destination_account_id, source",
		"id, fitid, type, date_posted, amount, name, memo, account_id, family_id, created_by, imported_at, manually_tagged, status, is_transfer, destination_account_id, source",
		"FROM transactions WHERE family_id = '%[1]s'"},
	{"transaction_tags", "transaction_id, tag_id", "transaction_id, tag_id",
		"FROM transaction_tags WHERE transaction_id IN (SELECT id FROM transactions WHERE family_id = '%[1]s') AND tag_id IN (" + dumpTagIDs + ")"},
	// Estado do classificador: o da família + o global/"master" (family_id NULL), que serve a todo o sistema.
	{"classifier_states", "id, family_id, total_docs, class_docs, class_word_counts, class_total_words, vocabulary, updated_at", "id, family_id, total_docs, class_docs, class_word_counts, class_total_words, vocabulary, updated_at",
		"FROM classifier_states WHERE family_id = '%[1]s' OR family_id IS NULL"},
	{"merchant_mappings", "id, family_id, pattern, tag_id, created_at, updated_at", "id, family_id, pattern, tag_id, created_at, updated_at",
		"FROM merchant_mappings WHERE family_id = '%[1]s'"},
}

// WriteFamilyDump escreve em w um script psql: DDL completo + COPY ... FROM stdin de cada
// tabela filtrado pela família, lido num snapshot único (REPEATABLE READ, somente leitura).
func (r *DumpRepository) WriteFamilyDump(ctx context.Context, w io.Writer, familyID uuid.UUID) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	fid := familyID.String()
	if _, err := fmt.Fprintf(w, "-- Finager: dump da família %s\n\\set ON_ERROR_STOP on\n\n%s\n\n", fid, database.Schema); err != nil {
		return err
	}

	pgConn := tx.Conn().PgConn()
	for _, t := range dumpTables {
		if _, err := fmt.Fprintf(w, "COPY %s (%s) FROM stdin;\n", t.name, t.cols); err != nil {
			return err
		}
		query := strings.ReplaceAll("COPY (SELECT "+t.sel+" "+t.filter+") TO STDOUT", "%[1]s", fid)
		if _, err := pgConn.CopyTo(ctx, w, query); err != nil {
			return fmt.Errorf("dump de %s: %w", t.name, err)
		}
		if _, err := io.WriteString(w, "\\.\n\n"); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, DumpEndMarker+"\n")
	return err
}
