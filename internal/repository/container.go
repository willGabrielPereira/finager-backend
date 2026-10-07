package repository

import (
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
	SignupInvites    *SignupInviteRepository
	Coupons          *CouponRepository
	PasswordResets   *PasswordResetRepository
	Audit            *AuditRepository
	Dumps            *DumpRepository
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
		SignupInvites:    NewSignupInviteRepository(pool),
		Coupons:          NewCouponRepository(pool),
		PasswordResets:   NewPasswordResetRepository(pool),
		Audit:            NewAuditRepository(pool),
		Dumps:            NewDumpRepository(pool),
	}
}

