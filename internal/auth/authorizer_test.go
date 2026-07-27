package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

type roleResult struct {
	roles []Role
	err   error
}

type fakeRoleQuerier struct {
	results []roleResult
	calls   int
}

func (f *fakeRoleQuerier) ActiveUserRoles(_ context.Context, _ uuid.UUID) ([]Role, error) {
	r := f.results[f.calls]
	f.calls++
	return r.roles, r.err
}

func TestAuthorizerRequireAnyRole(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	tests := []struct {
		name       string
		seedUserID bool
		result     roleResult
		allowed    []Role
		wantStatus int
		wantNext   bool
	}{
		{
			name:       "missing user id in context is unauthorized",
			seedUserID: false,
			allowed:    []Role{ADMIN},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "authorization db error is internal server error",
			seedUserID: true,
			result:     roleResult{err: errors.New("db down")},
			allowed:    []Role{ADMIN},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "missing or disabled user is unauthorized",
			seedUserID: true,
			result:     roleResult{err: pgx.ErrNoRows},
			allowed:    []Role{ADMIN},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "active user without a matching role is forbidden",
			seedUserID: true,
			result:     roleResult{roles: []Role{STUDENT}},
			allowed:    []Role{EXPERIMENTER, ADMIN},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "any matching role passes to next",
			seedUserID: true,
			result:     roleResult{roles: []Role{STUDENT, EXPERIMENTER}},
			allowed:    []Role{EXPERIMENTER, ADMIN},
			wantStatus: http.StatusOK,
			wantNext:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeRoleQuerier{results: []roleResult{tt.result}}
			authorizer := NewAuthorizer(querier, nil)

			nextCalled := false
			next := func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			}

			req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
			if tt.seedUserID {
				req = req.WithContext(ContextWithUserID(req.Context(), userID))
			}
			rec := httptest.NewRecorder()

			authorizer.RequireAnyRole(tt.allowed...)(next)(rec, req)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantNext, nextCalled)
		})
	}
}

func TestAuthorizerRequireAnyRoleReadsRolesEachRequest(t *testing.T) {
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	querier := &fakeRoleQuerier{results: []roleResult{
		{roles: []Role{EXPERIMENTER}},
		{roles: []Role{STUDENT}},
	}}
	authorizer := NewAuthorizer(querier, nil)

	call := func() int {
		next := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
		req := httptest.NewRequest(http.MethodGet, "/api/questions/x/answers", nil)
		req = req.WithContext(ContextWithUserID(req.Context(), userID))
		rec := httptest.NewRecorder()
		authorizer.RequireAnyRole(EXPERIMENTER, ADMIN)(next)(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, call())
	require.Equal(t, http.StatusForbidden, call())
	require.Equal(t, 2, querier.calls)
}
