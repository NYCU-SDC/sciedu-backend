package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sciedu-backend/internal/auth"

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHandler(repo *fakeRepo) *Handler {
	return NewHandler(NewService(repo, nil), nil)
}

// injectMux registers the handler methods directly, seeding the caller's user ID
// into the request context but without the RequireAnyRole gate, so handler logic
// can be tested in isolation from authorization.
func injectMux(h *Handler, actorID uuid.UUID) *http.ServeMux {
	mux := http.NewServeMux()
	inject := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fn(w, r.WithContext(auth.ContextWithUserID(r.Context(), actorID)))
		}
	}
	mux.HandleFunc("GET /api/users/me", inject(h.Me))
	mux.HandleFunc("GET /api/users", inject(h.List))
	mux.HandleFunc("GET /api/users/{id}", inject(h.Get))
	mux.HandleFunc("DELETE /api/users/{id}", inject(h.Delete))
	mux.HandleFunc("PUT /api/users/{id}/roles", inject(h.UpdateRoles))
	return mux
}

type fakeRoleQuerier struct {
	roles []auth.Role
}

func (f fakeRoleQuerier) ActiveUserRoles(ctx context.Context, userID uuid.UUID) ([]auth.Role, error) {
	return f.roles, nil
}

func sampleUser(id uuid.UUID) Profile {
	return Profile{ID: id, Email: "a@b.com", Name: "Alice", Roles: []string{"STUDENT"}}
}

func TestHandlerMe(t *testing.T) {
	actor := uuid.New()

	tests := []struct {
		name     string
		getFn    func(ctx context.Context, id uuid.UUID) (Profile, error)
		wantCode int
	}{
		{
			name:     "returns the caller profile",
			getFn:    func(ctx context.Context, id uuid.UUID) (Profile, error) { return sampleUser(id), nil },
			wantCode: http.StatusOK,
		},
		{
			name:     "missing user is 404",
			getFn:    func(ctx context.Context, id uuid.UUID) (Profile, error) { return Profile{}, pgx.ErrNoRows },
			wantCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{getActiveUserFn: tt.getFn}
			mux := injectMux(newHandler(repo), actor)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users/me", nil))

			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode == http.StatusOK {
				var body userResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, actor, body.ID)
			}
		})
	}
}

func TestHandlerList(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantCode   int
		wantSearch *string
		wantRole   *string
	}{
		{name: "defaults", query: "", wantCode: http.StatusOK},
		{name: "page below one is rejected", query: "?page=0", wantCode: http.StatusBadRequest},
		{name: "non-numeric page is rejected", query: "?page=abc", wantCode: http.StatusBadRequest},
		{name: "page size above max is rejected", query: "?pageSize=101", wantCode: http.StatusBadRequest},
		{name: "page size below one is rejected", query: "?pageSize=0", wantCode: http.StatusBadRequest},
		{name: "unknown role filter is rejected", query: "?role=BOGUS", wantCode: http.StatusBadRequest},
		{name: "search and role pass through", query: "?search=alice&role=ADMIN", wantCode: http.StatusOK, wantSearch: strPtr("alice"), wantRole: strPtr("ADMIN")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{
				listUsersFn: func(ctx context.Context, params ListParams) ([]Profile, error) {
					return []Profile{sampleUser(uuid.New())}, nil
				},
				countUsersFn: func(ctx context.Context, filter ListFilter) (int64, error) { return 1, nil },
			}
			mux := injectMux(newHandler(repo), uuid.New())

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/users"+tt.query, nil))

			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode != http.StatusOK {
				return
			}

			var body paginatedUsersResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Len(t, body.Items, 1)

			if tt.wantSearch != nil {
				require.NotNil(t, repo.lastListParams.Search)
				assert.Equal(t, *tt.wantSearch, *repo.lastListParams.Search)
			}
			if tt.wantRole != nil {
				require.NotNil(t, repo.lastListParams.Role)
				assert.Equal(t, *tt.wantRole, *repo.lastListParams.Role)
			}
		})
	}
}

func TestHandlerGet(t *testing.T) {
	target := uuid.New()

	tests := []struct {
		name     string
		path     string
		getFn    func(ctx context.Context, id uuid.UUID) (Profile, error)
		wantCode int
	}{
		{
			name:     "returns the user",
			path:     "/api/users/" + target.String(),
			getFn:    func(ctx context.Context, id uuid.UUID) (Profile, error) { return sampleUser(id), nil },
			wantCode: http.StatusOK,
		},
		{
			name:     "malformed id is 400",
			path:     "/api/users/not-a-uuid",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing user is 404",
			path:     "/api/users/" + target.String(),
			getFn:    func(ctx context.Context, id uuid.UUID) (Profile, error) { return Profile{}, pgx.ErrNoRows },
			wantCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{getActiveUserFn: tt.getFn}
			mux := injectMux(newHandler(repo), uuid.New())

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

func TestHandlerDelete(t *testing.T) {
	actor := uuid.New()
	other := uuid.New()

	tests := []struct {
		name         string
		targetID     uuid.UUID
		path         string
		softDeleteFn func(ctx context.Context, id uuid.UUID) error
		wantCode     int
	}{
		{name: "deleting self is forbidden", path: "/api/users/" + actor.String(), wantCode: http.StatusForbidden},
		{name: "deleting another user succeeds", path: "/api/users/" + other.String(), wantCode: http.StatusNoContent},
		{name: "malformed id is 400", path: "/api/users/not-a-uuid", wantCode: http.StatusBadRequest},
		{
			name:         "missing user is 404",
			path:         "/api/users/" + other.String(),
			softDeleteFn: func(ctx context.Context, id uuid.UUID) error { return pgx.ErrNoRows },
			wantCode:     http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{softDeleteFn: tt.softDeleteFn}
			mux := injectMux(newHandler(repo), actor)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, tt.path, nil))

			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

func TestHandlerUpdateRoles(t *testing.T) {
	actor := uuid.New()
	other := uuid.New()

	tests := []struct {
		name      string
		path      string
		body      string
		replaceFn func(ctx context.Context, id uuid.UUID, roles []string) (Profile, error)
		wantCode  int
	}{
		{
			name: "replaces another user's roles",
			path: "/api/users/" + other.String() + "/roles",
			body: `{"roles":["EXPERIMENTER"]}`,
			replaceFn: func(ctx context.Context, id uuid.UUID, roles []string) (Profile, error) {
				return Profile{ID: id, Roles: roles}, nil
			},
			wantCode: http.StatusOK,
		},
		{name: "changing own roles is forbidden", path: "/api/users/" + actor.String() + "/roles", body: `{"roles":["ADMIN"]}`, wantCode: http.StatusForbidden},
		{name: "empty roles is rejected", path: "/api/users/" + other.String() + "/roles", body: `{"roles":[]}`, wantCode: http.StatusBadRequest},
		{name: "unknown role is rejected", path: "/api/users/" + other.String() + "/roles", body: `{"roles":["BOSS"]}`, wantCode: http.StatusBadRequest},
		{name: "duplicate roles are rejected", path: "/api/users/" + other.String() + "/roles", body: `{"roles":["ADMIN","ADMIN"]}`, wantCode: http.StatusBadRequest},
		{
			name: "missing user is 404",
			path: "/api/users/" + other.String() + "/roles",
			body: `{"roles":["ADMIN"]}`,
			replaceFn: func(ctx context.Context, id uuid.UUID, roles []string) (Profile, error) {
				return Profile{}, pgx.ErrNoRows
			},
			wantCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{replaceRolesFn: tt.replaceFn}
			mux := injectMux(newHandler(repo), actor)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body)))

			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode == http.StatusOK {
				var body userResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, []string{"EXPERIMENTER"}, body.Roles)
			}
		})
	}
}

// TestRegisterRoutesAuthorization exercises the real RequireAnyRole wiring per the
// permission matrix: read routes need EXPERIMENTER/ADMIN, write routes need ADMIN,
// and /me only needs authentication.
func TestRegisterRoutesAuthorization(t *testing.T) {
	actor := uuid.New()
	other := uuid.New()

	authorizedMux := func(roles []auth.Role) *http.ServeMux {
		repo := &fakeRepo{
			listUsersFn:     func(ctx context.Context, params ListParams) ([]Profile, error) { return nil, nil },
			countUsersFn:    func(ctx context.Context, filter ListFilter) (int64, error) { return 0, nil },
			getActiveUserFn: func(ctx context.Context, id uuid.UUID) (Profile, error) { return sampleUser(id), nil },
			softDeleteFn:    func(ctx context.Context, id uuid.UUID) error { return nil },
			replaceRolesFn: func(ctx context.Context, id uuid.UUID, r []string) (Profile, error) {
				return Profile{ID: id, Roles: r}, nil
			},
		}
		set := middlewareutil.NewSet(func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				next(w, r.WithContext(auth.ContextWithUserID(r.Context(), actor)))
			}
		})
		mux := http.NewServeMux()
		newHandler(repo).RegisterRoutes(mux, set, auth.NewAuthorizer(fakeRoleQuerier{roles: roles}, nil))
		return mux
	}

	tests := []struct {
		name     string
		roles    []auth.Role
		method   string
		path     string
		body     string
		wantCode int
	}{
		{name: "student can read own profile", roles: []auth.Role{auth.STUDENT}, method: http.MethodGet, path: "/api/users/me", wantCode: http.StatusOK},
		{name: "student cannot list", roles: []auth.Role{auth.STUDENT}, method: http.MethodGet, path: "/api/users", wantCode: http.StatusForbidden},
		{name: "experimenter can list", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodGet, path: "/api/users", wantCode: http.StatusOK},
		{name: "experimenter can get", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodGet, path: "/api/users/" + other.String(), wantCode: http.StatusOK},
		{name: "experimenter cannot delete", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodDelete, path: "/api/users/" + other.String(), wantCode: http.StatusForbidden},
		{name: "experimenter cannot update roles", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodPut, path: "/api/users/" + other.String() + "/roles", body: `{"roles":["ADMIN"]}`, wantCode: http.StatusForbidden},
		{name: "admin can delete another user", roles: []auth.Role{auth.ADMIN}, method: http.MethodDelete, path: "/api/users/" + other.String(), wantCode: http.StatusNoContent},
		{name: "admin can update another user's roles", roles: []auth.Role{auth.ADMIN}, method: http.MethodPut, path: "/api/users/" + other.String() + "/roles", body: `{"roles":["ADMIN"]}`, wantCode: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := authorizedMux(tt.roles)
			var reqBody *strings.Reader
			if tt.body != "" {
				reqBody = strings.NewReader(tt.body)
			} else {
				reqBody = strings.NewReader("")
			}

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, reqBody))

			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}
