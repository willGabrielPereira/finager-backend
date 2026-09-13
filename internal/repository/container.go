package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Container agrupa todos os repositórios da aplicação.
// Isso evita que o main.go ou outros inicializadores precisem instanciar
// dezenas de repositórios individualmente no futuro.
type Container struct {
	Transactions  *TransactionRepository
	Users         *UserRepository
	Families      *FamilyRepository
	RefreshTokens *RefreshTokenRepository
	Blocklist     *BlocklistRepository
	Tags             *TagRepository
	Accounts         *AccountRepository
	ClassifierStates *ClassifierStateRepository
	MerchantMappings *MerchantMappingRepository
	Invites          *FamilyInviteRepository
}

// New cria um container já com todos os repositórios injetados com o banco de dados.
func New(pool *pgxpool.Pool) *Container {
	return &Container{
		Transactions:     NewTransactionRepository(pool),
		Users:            NewUserRepository(pool),
		Families:         NewFamilyRepository(pool),
		RefreshTokens:    NewRefreshTokenRepository(pool),
		Blocklist:        NewBlocklistRepository(pool),
		Tags:             NewTagRepository(pool),
		Accounts:         NewAccountRepository(pool),
		ClassifierStates: NewClassifierStateRepository(pool),
		MerchantMappings: NewMerchantMappingRepository(pool),
		Invites:          NewFamilyInviteRepository(pool),
	}
}

// EnsureIndexes executa a migração idempotente de colunas, tabelas e índices.
func (c *Container) EnsureIndexes(ctx context.Context) error {
	queries := []string{
		`ALTER TABLE transactions ADD COLUMN IF NOT EXISTS manually_tagged BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE transactions ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'POSTED'`,
		`ALTER TABLE transactions ADD COLUMN IF NOT EXISTS is_transfer BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE transactions ADD COLUMN IF NOT EXISTS destination_account_id UUID REFERENCES accounts(id) ON DELETE SET NULL`,
		`ALTER TABLE transactions ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'OFX'`,
		`ALTER TABLE accounts ADD COLUMN IF NOT EXISTS type TEXT NOT NULL DEFAULT 'CHECKING'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT UNIQUE`,
		`CREATE TABLE IF NOT EXISTS merchant_mappings (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			family_id  UUID NOT NULL REFERENCES families(id) ON DELETE CASCADE,
			pattern    TEXT NOT NULL,
			tag_id     UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (family_id, pattern)
		)`,
		`CREATE TABLE IF NOT EXISTS family_invites (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			family_id    UUID NOT NULL REFERENCES families(id) ON DELETE CASCADE,
			token        TEXT NOT NULL UNIQUE,
			target_email TEXT,
			created_by   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
			expires_at   TIMESTAMPTZ NOT NULL,
			used_at      TIMESTAMPTZ,
			used_by      UUID REFERENCES users(id) ON DELETE SET NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_family_date ON transactions(family_id, date_posted DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_account ON transactions(account_id)`,
		`CREATE INDEX IF NOT EXISTS idx_transactions_status ON transactions(family_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_merchant_mappings_family ON merchant_mappings(family_id)`,
		`CREATE INDEX IF NOT EXISTS idx_family_invites_token ON family_invites(token)`,
		`CREATE INDEX IF NOT EXISTS idx_family_invites_family ON family_invites(family_id)`,
	}

	for _, q := range queries {
		if _, err := c.Transactions.pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

