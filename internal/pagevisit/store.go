package pagevisit

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type transactionDB interface {
	DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Store struct {
	*Queries
	db transactionDB
}

func NewStore(db transactionDB) *Store {
	return &Store{
		Queries: New(db),
		db:      db,
	}
}

func (s *Store) WithinTx(ctx context.Context, fn func(TransactionRepository) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(s.WithTx(tx)); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
