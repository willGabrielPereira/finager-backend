package migrations

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type M004FillEmptyTransactionNames struct{}

func (m *M004FillEmptyTransactionNames) ID() string {
	return "004_fill_empty_transaction_names"
}

func (m *M004FillEmptyTransactionNames) Description() string {
	return "Preenche o campo name com memo em transações com nome em branco (extratos Nubank e similares)"
}

func (m *M004FillEmptyTransactionNames) Up(ctx context.Context, pool *pgxpool.Pool) error {
	query := `
	UPDATE transactions
	SET name = memo
	WHERE (name = '' OR name IS NULL)
	  AND memo != ''
	  AND memo IS NOT NULL;
	`
	_, err := pool.Exec(ctx, query)
	return err
}

func init() {
	Register(&M004FillEmptyTransactionNames{})
}
