package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMiddlewareAccessToken(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	repo := &fakeAuthRepository{}
	svc := NewService(repo, ServiceConfig{
		Secret:      "test-secret",
		Environment: EnvironmentDev,
		Now:         func() time.Time { return now },
	}, nil)
	session, err := svc.IssueSession(t.Context(), IssueSessionParams{UserID: userID})
	require.NoError(t, err)

	tests := []struct {
		name       string
		cookie     *http.Cookie
		wantStatus int
		wantNext   bool
	}{
		{
			name:       "valid access token injects user id",
			cookie:     &http.Cookie{Name: accessTokenCookieName, Value: session.AccessToken},
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
		{
			name:       "missing access token returns unauthorized problem",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid access token returns unauthorized problem",
			cookie:     &http.Cookie{Name: accessTokenCookieName, Value: "not-a-jwt"},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calledNext := false
			middleware := NewMiddleware(svc, nil)
			next := middleware.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calledNext = true
				gotUserID, ok := UserIDFromContext(r.Context())
				require.True(t, ok)
				require.Equal(t, userID, gotUserID)
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/api/protected", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()

			next(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantNext, calledNext)
			if !tt.wantNext {
				require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			}
		})
	}
}
