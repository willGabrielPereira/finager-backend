package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type M008SupportAccess struct{}

func (m *M008SupportAccess) ID() string {
	return "008_support_access"
}

func (m *M008SupportAccess) Description() string {
	return "Adiciona concessão de acesso do suporte à família e o log de auditoria de ações administrativas"
}

func (m *M008SupportAccess) Up(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
	-- NULL ou data passada = suporte sem acesso aos dados da família.
	ALTER TABLE families ADD COLUMN IF NOT EXISTS support_access_until TIMESTAMPTZ;

	CREATE TABLE IF NOT EXISTS admin_audit_log (
		id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		admin_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		family_id  UUID        NOT NULL REFERENCES families(id) ON DELETE CASCADE,
		action     TEXT        NOT NULL,
		reason     TEXT        NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE INDEX IF NOT EXISTS idx_admin_audit_log_family ON admin_audit_log(family_id, created_at DESC);
	`
	_, err := pool.Exec(ctx, query)
	return err
}

func init() {
	Register(&M008SupportAccess{})
}
