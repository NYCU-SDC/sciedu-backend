package auth

import (
	"context"
	"net/http"
	"time"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type AccessTokenVerifier interface {
	VerifyAccessToken(rawToken string) (AccessTokenClaims, error)
}

type Middleware struct {
	verifier      AccessTokenVerifier
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
}

type contextKey string

const (
	userIDContextKey    contextKey = "auth.user_id"
	accessExpContextKey contextKey = "auth.access_exp"
)

func NewMiddleware(verifier AccessTokenVerifier, logger *zap.Logger) Middleware {
	if logger == nil {
		logger = zap.NewNop()
	}
	return Middleware{
		verifier:      verifier,
		logger:        logger,
		problemWriter: problemutil.New(),
	}
}

func (m Middleware) HandlerFunc(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger := logutil.WithContext(ctx, m.logger)
		rawToken, err := cookieValue(r, accessTokenCookieName)
		if err != nil {
			m.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
			return
		}

		claims, err := m.verifier.VerifyAccessToken(rawToken)
		if err != nil {
			m.problemWriter.WriteError(ctx, w, err, logger)
			return
		}

		ctx = context.WithValue(ctx, userIDContextKey, claims.UserID)
		ctx = context.WithValue(ctx, accessExpContextKey, claims.ExpiresAt)
		next(w, r.WithContext(ctx))
	}
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(userIDContextKey).(uuid.UUID)
	return userID, ok
}

func AccessTokenExpiresAtFromContext(ctx context.Context) (time.Time, bool) {
	expiresAt, ok := ctx.Value(accessExpContextKey).(time.Time)
	return expiresAt, ok
}
