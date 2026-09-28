package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type M006AdminRolesAndInvites struct{}

func (m *M006AdminRolesAndInvites) ID() string {
	return "006_admin_roles_and_invites"
}

func (m *M006AdminRolesAndInvites) Description() string {
	return "Adiciona papéis administrativos e último login ao usuário, ativação de cupons e convites de cadastro"
}

func (m *M006AdminRolesAndInvites) Up(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
	-- 1. Papel administrativo e último login no usuário
	ALTER TABLE users
	ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'user',
	ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;

	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_constraint WHERE conname = 'chk_users_role'
		) THEN
			ALTER TABLE users ADD CONSTRAINT chk_users_role CHECK (role IN ('user','moderator','admin'));
		END IF;
	END $$;

	-- 2. Ativação de cupons
	ALTER TABLE coupons ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT true;

	-- 3. Convites de cadastro
	CREATE TABLE IF NOT EXISTS signup_invites (
		id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		token              VARCHAR(64) UNIQUE NOT NULL,
		plan_granted       VARCHAR(20) NOT NULL DEFAULT 'LIFETIME_FREE',
		created_by         UUID NOT NULL REFERENCES users(id),
		expires_at         TIMESTAMPTZ NOT NULL,
		used_at            TIMESTAMPTZ,
		used_by_family_id  UUID REFERENCES families(id),
		created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE INDEX IF NOT EXISTS idx_signup_invites_token ON signup_invites(token);
	`
	_, err := pool.Exec(ctx, query)
	return err
}

func init() {
	Register(&M006AdminRolesAndInvites{})
}
