package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type M005UserOnboardingAndFamilyPlans struct{}

func (m *M005UserOnboardingAndFamilyPlans) ID() string {
	return "005_user_onboarding_and_family_plans"
}

func (m *M005UserOnboardingAndFamilyPlans) Description() string {
	return "Adiciona campos de onboarding no usuário, controle de planos nas famílias e tabela de cupons"
}

func (m *M005UserOnboardingAndFamilyPlans) Up(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
	-- 1. Campos de Onboarding no Usuário
	ALTER TABLE users 
	ADD COLUMN IF NOT EXISTS onboarding_completed BOOLEAN NOT NULL DEFAULT false,
	ADD COLUMN IF NOT EXISTS onboarding_step INT NOT NULL DEFAULT 0;

	-- 2. Campos de Plano e Assinatura na Família
	ALTER TABLE families
	ADD COLUMN IF NOT EXISTS plan TEXT NOT NULL DEFAULT 'FREE',
	ADD COLUMN IF NOT EXISTS subscription_status TEXT NOT NULL DEFAULT 'ACTIVE',
	ADD COLUMN IF NOT EXISTS subscription_expires_at TIMESTAMPTZ,
	ADD COLUMN IF NOT EXISTS subscription_provider TEXT,
	ADD COLUMN IF NOT EXISTS external_subscription_id TEXT;

	CREATE INDEX IF NOT EXISTS idx_families_plan ON families(plan);

	-- 3. Tabela de Cupons Promocionais
	CREATE TABLE IF NOT EXISTS coupons (
		id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		code             TEXT NOT NULL UNIQUE,
		discount_percent INT NOT NULL,
		plan_granted     TEXT NOT NULL DEFAULT 'PRO',
		max_uses         INT,
		times_used       INT NOT NULL DEFAULT 0,
		expires_at       TIMESTAMPTZ,
		created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
	);

	CREATE INDEX IF NOT EXISTS idx_coupons_code ON coupons(lower(code));

	-- 4. Cupom inicial para testes e convidados especiais (100% de desconto perpétuo)
	INSERT INTO coupons (code, discount_percent, plan_granted)
	VALUES ('AMIGO100', 100, 'LIFETIME_FREE')
	ON CONFLICT (code) DO NOTHING;
	`
	_, err := pool.Exec(ctx, query)
	return err
}

func init() {
	Register(&M005UserOnboardingAndFamilyPlans{})
}
