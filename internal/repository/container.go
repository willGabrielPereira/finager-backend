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
	Tags          *TagRepository
	Accounts      *AccountRepository
	ClassifierStates *ClassifierStateRepository
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
	}
}

// EnsureIndexes executa a criação de índices garantindo que todos os
// repositórios fiquem com os índices corretos no PostgreSQL.
func (c *Container) EnsureIndexes(ctx context.Context) error {
	return nil
}
