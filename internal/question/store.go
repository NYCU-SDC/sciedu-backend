package question

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

func (s *Store) WithinTx(ctx context.Context, fn func(QuestionQuerier, OptionQuerier) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	q := s.WithTx(tx)
	if err := fn(q, q); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
