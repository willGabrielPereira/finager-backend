package repository

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
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
	TagRules      *TagRuleRepository
	Accounts      *AccountRepository
}

// New cria um container já com todos os repositórios injetados com o banco de dados.
func New(db *mongo.Database) *Container {
	return &Container{
		Transactions:  NewTransactionRepository(db),
		Users:         NewUserRepository(db),
		Families:      NewFamilyRepository(db),
		RefreshTokens: NewRefreshTokenRepository(db),
		Blocklist:     NewBlocklistRepository(db),
		Tags:          NewTagRepository(db),
		TagRules:      NewTagRuleRepository(db),
		Accounts:      NewAccountRepository(db),
	}
}

// EnsureIndexes executa a criação de índices garantindo que todos os
// repositórios fiquem com os índices corretos no MongoDB.
func (c *Container) EnsureIndexes(ctx context.Context) error {
	if err := c.Users.EnsureIndexes(ctx); err != nil {
		return err
	}
	if err := c.RefreshTokens.EnsureIndexes(ctx); err != nil {
		return err
	}
	if err := c.Blocklist.EnsureIndexes(ctx); err != nil {
		return err
	}
	if err := c.TagRules.EnsureIndexes(ctx); err != nil {
		return err
	}
	return nil
}
