package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type M007EmailFeatures struct{}

func (m *M007EmailFeatures) ID() string {
	return "007_email_features"
}

func (m *M007EmailFeatures) Description() string {
	return "Cria password_reset_tokens, opt-out de lembretes por e-mail e suporte ao lembrete OFX"
}

func (m *M007EmailFeatures) Up(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
	-- 1. Tokens de recuperação de senha
	CREATE TABLE IF NOT EXISTS password_reset_tokens (
		id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL UNIQUE,
		expires_at TIMESTAMPTZ NOT NULL,
		used_at    TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user ON password_reset_tokens(user_id);

	-- 2. Opt-out de lembretes por e-mail
	ALTER TABLE users ADD COLUMN IF NOT EXISTS email_reminders_opt_out BOOLEAN NOT NULL DEFAULT false;

	-- 3. Controle do lembrete OFX (1 e-mail por família).
	-- Sem backfill: intencional — famílias já inativas hoje são justamente o
	-- público-alvo do lembrete, e um backfill as silenciaria para sempre.
	ALTER TABLE families ADD COLUMN IF NOT EXISTS ofx_reminder_sent_at TIMESTAMPTZ;

	CREATE INDEX IF NOT EXISTS idx_transactions_family_ofx_imported ON transactions(family_id, imported_at DESC) WHERE source = 'OFX';
	`
	_, err := pool.Exec(ctx, query)
	return err
}

func init() {
	Register(&M007EmailFeatures{})
}
