package page

import (
	"context"
	"errors"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
)

var errInvalidPagePayload = errors.New("invalid page payload")

var errInvalidBlockPayload = errors.New("invalid page block payload")

// wrapTransactionError maps errors that can first surface when a deferred
// constraint is checked at commit. Errors already mapped inside the callback,
// including validation sentinels, pass through unchanged.
func wrapTransactionError(err error, logger *zap.Logger, operation string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) || errors.Is(err, context.DeadlineExceeded) {
		return databaseutil.WrapDBError(err, logger, operation)
	}
	return err
}
