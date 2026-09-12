package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type M002TransactionEnhancements struct{}

func (m *M002TransactionEnhancements) ID() string {
	return "002_transaction_enhancements"
}

func (m *M002TransactionEnhancements) Description() string {
	return "Adiciona campos de reconciliação, status, blindagem de tags e memória de comerciantes"
}

func (m *M002TransactionEnhancements) Up(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
	ALTER TABLE transactions ADD COLUMN IF NOT EXISTS manually_tagged BOOLEAN NOT NULL DEFAULT false;
	ALTER TABLE transactions ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'POSTED';
	ALTER TABLE transactions ADD COLUMN IF NOT EXISTS is_transfer BOOLEAN NOT NULL DEFAULT false;
	ALTER TABLE transactions ADD COLUMN IF NOT EXISTS destination_account_id UUID REFERENCES accounts(id) ON DELETE SET NULL;
	ALTER TABLE transactions ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'OFX';

	CREATE TABLE IF NOT EXISTS merchant_mappings (
		id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		family_id  UUID NOT NULL REFERENCES families(id) ON DELETE CASCADE,
		pattern    TEXT NOT NULL,
		tag_id     UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		UNIQUE (family_id, pattern)
	);

	CREATE INDEX IF NOT EXISTS idx_transactions_status ON transactions(family_id, status);
	CREATE INDEX IF NOT EXISTS idx_merchant_mappings_family ON merchant_mappings(family_id);
	`
	_, err := pool.Exec(ctx, query)
	return err
}

func init() {
	Register(&M002TransactionEnhancements{})
}
