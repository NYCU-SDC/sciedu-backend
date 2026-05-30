package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
)

type contextKey string

const userIDKey contextKey = "userID"

var ErrMissingUserID = errors.New("user ID not found in context")

func SetUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

func GetUserID(ctx context.Context) (uuid.UUID, error) {
	id, ok := ctx.Value(userIDKey).(uuid.UUID)
	if !ok || id == uuid.Nil {
		return uuid.Nil, ErrMissingUserID
	}
	return id, nil
}

// MockAuthMiddleware returns a middleware that injects the given userID into the context.
// Replace with real JWT middleware when auth is implemented.
func MockAuthMiddleware(userID uuid.UUID) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx := SetUserID(r.Context(), userID)
			next(w, r.WithContext(ctx))
		}
	}
}
