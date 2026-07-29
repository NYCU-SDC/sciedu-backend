package auth

import (
	"context"
	"errors"
	"net/http"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

type Role string

const (
	STUDENT      Role = "STUDENT"
	EXPERIMENTER Role = "EXPERIMENTER"
	ADMIN        Role = "ADMIN"
)

type RoleQuerier interface {
	ActiveUserRoles(ctx context.Context, userID uuid.UUID) ([]Role, error)
}

type Authorizer struct {
	roles         RoleQuerier
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
}

func NewAuthorizer(roles RoleQuerier, logger *zap.Logger) *Authorizer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Authorizer{
		roles:         roles,
		logger:        logger,
		problemWriter: problemutil.New(),
	}
}

func (a *Authorizer) RequireAnyRole(allowed ...Role) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			logger := logutil.WithContext(ctx, a.logger)

			userID, ok := UserIDFromContext(ctx)
			if !ok {
				a.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
				return
			}

			roles, err := a.roles.ActiveUserRoles(ctx, userID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					a.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
					return
				}
				a.problemWriter.WriteError(ctx, w, err, logger)
				return
			}

			if !hasAnyRole(roles, allowed) {
				a.problemWriter.WriteError(ctx, w, handlerutil.ErrForbidden, logger)
				return
			}

			next(w, r)
		}
	}
}

func hasAnyRole(userRoles, allowed []Role) bool {
	for _, want := range allowed {
		for _, have := range userRoles {
			if have == want {
				return true
			}
		}
	}
	return false
}
