package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type M001InitialSchema struct{}

func (m *M001InitialSchema) ID() string {
	return "001_initial_schema"
}

func (m *M001InitialSchema) Description() string {
	return "Criação das tabelas fundamentais do sistema Finager"
}

func (m *M001InitialSchema) Up(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
	CREATE TABLE IF NOT EXISTS families (
		id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name       TEXT        NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS users (
		id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		login         TEXT        NOT NULL UNIQUE,
		password_hash TEXT        NOT NULL,
		family_id     UUID        NOT NULL REFERENCES families(id) ON DELETE CASCADE,
		created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS family_members (
		family_id UUID NOT NULL REFERENCES families(id) ON DELETE CASCADE,
		user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		PRIMARY KEY (family_id, user_id)
	);

	CREATE TABLE IF NOT EXISTS tags (
		id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name       TEXT        NOT NULL,
		color      TEXT        NOT NULL DEFAULT '#6B7280',
		icon       TEXT        NOT NULL DEFAULT 'tag',
		family_id  UUID        REFERENCES families(id) ON DELETE CASCADE,
		is_system  BOOLEAN     NOT NULL DEFAULT false,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS accounts (
		id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name        TEXT        NOT NULL,
		institution TEXT        NOT NULL,
		family_id   UUID        NOT NULL REFERENCES families(id) ON DELETE CASCADE,
		created_by  UUID        NOT NULL REFERENCES users(id),
		created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE TABLE IF NOT EXISTS account_allowed_users (
		account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		PRIMARY KEY (account_id, user_id)
	);

	CREATE TABLE IF NOT EXISTS transactions (
		id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		fitid       TEXT        NOT NULL,
		type        TEXT        NOT NULL,
		date_posted TIMESTAMPTZ NOT NULL,
		amount      NUMERIC(15,2) NOT NULL,
		name        TEXT        NOT NULL DEFAULT '',
		memo        TEXT        NOT NULL DEFAULT '',
		account_id  UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
		family_id   UUID        NOT NULL REFERENCES families(id) ON DELETE CASCADE,
		created_by  UUID        NOT NULL REFERENCES users(id),
		imported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		UNIQUE (fitid, account_id, family_id)
	);

	CREATE TABLE IF NOT EXISTS transaction_tags (
		transaction_id UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
		tag_id         UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		PRIMARY KEY (transaction_id, tag_id)
	);

	CREATE TABLE IF NOT EXISTS refresh_tokens (
		id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT        NOT NULL UNIQUE,
		expires_at TIMESTAMPTZ NOT NULL,
		revoked    BOOLEAN     NOT NULL DEFAULT false
	);

	CREATE TABLE IF NOT EXISTS blocklist (
		id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		token_hash TEXT        NOT NULL UNIQUE,
		expires_at TIMESTAMPTZ NOT NULL
	);

	CREATE TABLE IF NOT EXISTS classifier_states (
		id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		family_id         UUID        UNIQUE,
		total_docs        INT         NOT NULL DEFAULT 0,
		class_docs        JSONB       NOT NULL DEFAULT '{}',
		class_word_counts JSONB       NOT NULL DEFAULT '{}',
		class_total_words JSONB       NOT NULL DEFAULT '{}',
		vocabulary        JSONB       NOT NULL DEFAULT '[]',
		updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE INDEX IF NOT EXISTS idx_transactions_family_date ON transactions(family_id, date_posted DESC);
	CREATE INDEX IF NOT EXISTS idx_transactions_account ON transactions(account_id);
	CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
	CREATE INDEX IF NOT EXISTS idx_blocklist_expires ON blocklist(expires_at);
	CREATE INDEX IF NOT EXISTS idx_tags_family ON tags(family_id);
	`
	_, err := pool.Exec(ctx, query)
	return err
}

func init() {
	Register(&M001InitialSchema{})
}
