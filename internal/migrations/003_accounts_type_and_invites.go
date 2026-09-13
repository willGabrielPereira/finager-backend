package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type M003AccountsTypeAndInvites struct{}

func (m *M003AccountsTypeAndInvites) ID() string {
	return "003_accounts_type_and_invites"
}

func (m *M003AccountsTypeAndInvites) Description() string {
	return "Adiciona type em accounts, email em users e tabela de family_invites com tokens criptográficos"
}

func (m *M003AccountsTypeAndInvites) Up(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
	ALTER TABLE accounts ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'CHECKING';
	ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT UNIQUE;

	CREATE TABLE IF NOT EXISTS family_invites (
		id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		family_id    UUID NOT NULL REFERENCES families(id) ON DELETE CASCADE,
		token        TEXT NOT NULL UNIQUE,
		target_email TEXT,
		created_by   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
		expires_at   TIMESTAMPTZ NOT NULL,
		used_at      TIMESTAMPTZ,
		used_by      UUID REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_family_invites_token ON family_invites(token);
	CREATE INDEX IF NOT EXISTS idx_family_invites_family ON family_invites(family_id);
	`
	_, err := pool.Exec(ctx, query)
	return err
}

func init() {
	Register(&M003AccountsTypeAndInvites{})
}
