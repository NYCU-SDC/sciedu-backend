package course

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sciedu-backend/internal/auth"

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCourseHandler(repo *fakeRepository) *Handler {
	return newCourseHandlerWithAuthorization(
		repo,
		&fakeCourseRoleQuerier{roles: []auth.Role{auth.ADMIN}},
		&fakeStudentCourseAccessChecker{},
	)
}

func newCourseHandlerWithAuthorization(repo *fakeRepository, roles auth.RoleQuerier, access StudentCourseAccessChecker) *Handler {
	return NewHandler(NewService(repo, roles, access, nil), nil)
}

func handlerMux(handler *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/courses", handler.List)
	mux.HandleFunc("POST /api/courses", handler.Create)
	mux.HandleFunc("GET /api/courses/{id}", handler.Get)
	mux.HandleFunc("PUT /api/courses/{id}", handler.Update)
	mux.HandleFunc("PUT /api/courses/{id}/status", handler.UpdateStatus)
	return mux
}

func sampleCourse(id uuid.UUID) Record {
	return Record{ID: id, Code: "BIO101", Title: "Biology", Status: CourseStatusDRAFT}
}

func TestHandlerListCourses(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantCode   int
		wantStatus *CourseStatus
		wantSearch *string
	}{
		{name: "uses defaults", wantCode: http.StatusOK},
		{name: "passes filters", query: "?page=2&pageSize=10&status=PUBLISHED&search=biology", wantCode: http.StatusOK, wantStatus: statusPtr(CourseStatusPUBLISHED), wantSearch: stringPointer("biology")},
		{name: "rejects zero page", query: "?page=0", wantCode: http.StatusBadRequest},
		{name: "rejects overflowing page", query: "?page=999999999999999999", wantCode: http.StatusBadRequest},
		{name: "rejects overflowing offset", query: "?page=2147483647&pageSize=100", wantCode: http.StatusBadRequest},
		{name: "rejects large page size", query: "?pageSize=101", wantCode: http.StatusBadRequest},
		{name: "rejects unknown status", query: "?status=UNKNOWN", wantCode: http.StatusBadRequest},
		{name: "rejects a present empty search", query: "?search=", wantCode: http.StatusBadRequest},
		{name: "preserves a whitespace search allowed by TypeSpec", query: "?search=+++", wantCode: http.StatusOK, wantSearch: stringPointer("   ")},
		{name: "rejects long search", query: "?search=" + strings.Repeat("x", 201), wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{
				listFn:  func(context.Context, ListParams) ([]Record, error) { return []Record{sampleCourse(uuid.New())}, nil },
				countFn: func(context.Context, ListFilter) (int64, error) { return 1, nil },
			}
			recorder := httptest.NewRecorder()
			handlerMux(newCourseHandler(repo)).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/courses"+tt.query, nil))
			assert.Equal(t, tt.wantCode, recorder.Code)
			if tt.wantCode != http.StatusOK {
				return
			}
			var response paginatedCoursesResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Len(t, response.Items, 1)
			assert.Equal(t, tt.wantStatus, repo.lastListParams.Status)
			assert.Equal(t, tt.wantSearch, repo.lastListParams.Search)
		})
	}
}

func TestHandlerCreateCourse(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		createFn func(context.Context, CreateParams) (Record, error)
		wantCode int
	}{
		{
			name: "creates draft course", body: `{"code":"BIO101","title":"Biology"}`,
			createFn: func(ctx context.Context, params CreateParams) (Record, error) {
				return sampleCourse(uuid.New()), nil
			}, wantCode: http.StatusCreated,
		},
		{name: "rejects missing code", body: `{"title":"Biology"}`, wantCode: http.StatusBadRequest},
		{name: "rejects blank code in service", body: `{"code":"   ","title":"Biology"}`, wantCode: http.StatusBadRequest},
		{name: "rejects malformed json", body: `{`, wantCode: http.StatusBadRequest},
		{
			name: "maps duplicate code to conflict", body: `{"code":"BIO101","title":"Biology"}`,
			createFn: func(context.Context, CreateParams) (Record, error) {
				return Record{}, &pgconn.PgError{Code: "23505", ConstraintName: "courses_code_lower_unique"}
			}, wantCode: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{createFn: tt.createFn}
			recorder := httptest.NewRecorder()
			handlerMux(newCourseHandler(repo)).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/courses", strings.NewReader(tt.body)))
			assert.Equal(t, tt.wantCode, recorder.Code)
			if tt.wantCode == http.StatusCreated {
				var response courseResponse
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
				assert.Equal(t, CourseStatusDRAFT, response.Status)
			}
		})
	}
}

func TestHandlerGetCourse(t *testing.T) {
	actorID := uuid.New()
	id := uuid.New()
	dependencyErr := errors.New("experiment dependency unavailable")
	tests := []struct {
		name      string
		path      string
		seedActor bool
		roles     []auth.Role
		allowed   bool
		accessErr error
		byIDFn    func(context.Context, uuid.UUID) (Record, error)
		wantCode  int
	}{
		{name: "management role returns course", path: "/api/courses/" + id.String(), seedActor: true, roles: []auth.Role{auth.ADMIN}, byIDFn: func(context.Context, uuid.UUID) (Record, error) { return sampleCourse(id), nil }, wantCode: http.StatusOK},
		{name: "student allowed", path: "/api/courses/" + id.String(), seedActor: true, roles: []auth.Role{auth.STUDENT}, allowed: true, byIDFn: func(context.Context, uuid.UUID) (Record, error) { return sampleCourse(id), nil }, wantCode: http.StatusOK},
		{name: "student denied", path: "/api/courses/" + id.String(), seedActor: true, roles: []auth.Role{auth.STUDENT}, wantCode: http.StatusForbidden},
		{name: "student dependency error", path: "/api/courses/" + id.String(), seedActor: true, roles: []auth.Role{auth.STUDENT}, accessErr: dependencyErr, wantCode: http.StatusInternalServerError},
		{name: "missing actor is unauthorized", path: "/api/courses/" + id.String(), roles: []auth.Role{auth.ADMIN}, wantCode: http.StatusUnauthorized},
		{name: "rejects malformed id", path: "/api/courses/not-a-uuid", seedActor: true, roles: []auth.Role{auth.ADMIN}, wantCode: http.StatusBadRequest},
		{name: "returns not found", path: "/api/courses/" + id.String(), seedActor: true, roles: []auth.Role{auth.ADMIN}, byIDFn: func(context.Context, uuid.UUID) (Record, error) { return Record{}, pgx.ErrNoRows }, wantCode: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newCourseHandlerWithAuthorization(
				&fakeRepository{byIDFn: tt.byIDFn},
				&fakeCourseRoleQuerier{roles: tt.roles},
				&fakeStudentCourseAccessChecker{allowed: tt.allowed, err: tt.accessErr},
			)
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.seedActor {
				request = request.WithContext(auth.ContextWithUserID(request.Context(), actorID))
			}
			recorder := httptest.NewRecorder()
			handlerMux(handler).ServeHTTP(recorder, request)
			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}

func TestHandlerUpdateCourse(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name     string
		path     string
		body     string
		updateFn func(context.Context, UpdateParams) (Record, error)
		wantCode int
	}{
		{name: "updates metadata", path: "/api/courses/" + id.String(), body: `{"code":"BIO102","title":"Advanced Biology"}`, updateFn: func(context.Context, UpdateParams) (Record, error) {
			return Record{ID: id, Code: "BIO102", Title: "Advanced Biology", Status: CourseStatusDRAFT}, nil
		}, wantCode: http.StatusOK},
		{name: "rejects malformed id", path: "/api/courses/nope", body: `{"code":"BIO102","title":"Advanced Biology"}`, wantCode: http.StatusBadRequest},
		{name: "rejects missing title", path: "/api/courses/" + id.String(), body: `{"code":"BIO102"}`, wantCode: http.StatusBadRequest},
		{name: "returns not found", path: "/api/courses/" + id.String(), body: `{"code":"BIO102","title":"Advanced Biology"}`, updateFn: func(context.Context, UpdateParams) (Record, error) { return Record{}, pgx.ErrNoRows }, wantCode: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handlerMux(newCourseHandler(&fakeRepository{updateFn: tt.updateFn})).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body)))
			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}

func TestHandlerUpdateCourseStatus(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name           string
		path           string
		body           string
		updateStatusFn func(context.Context, uuid.UUID, CourseStatus) (Record, error)
		wantCode       int
	}{
		{name: "updates status", path: "/api/courses/" + id.String() + "/status", body: `{"status":"PUBLISHED"}`, updateStatusFn: func(context.Context, uuid.UUID, CourseStatus) (Record, error) {
			return Record{ID: id, Status: CourseStatusPUBLISHED}, nil
		}, wantCode: http.StatusOK},
		{name: "rejects unknown status", path: "/api/courses/" + id.String() + "/status", body: `{"status":"UNKNOWN"}`, wantCode: http.StatusBadRequest},
		{name: "rejects malformed id", path: "/api/courses/nope/status", body: `{"status":"PUBLISHED"}`, wantCode: http.StatusBadRequest},
		{name: "returns not found", path: "/api/courses/" + id.String() + "/status", body: `{"status":"ARCHIVED"}`, updateStatusFn: func(context.Context, uuid.UUID, CourseStatus) (Record, error) { return Record{}, pgx.ErrNoRows }, wantCode: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handlerMux(newCourseHandler(&fakeRepository{updateStatusFn: tt.updateStatusFn})).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body)))
			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}

func TestRegisterCourseRoutesAuthorization(t *testing.T) {
	actorID := uuid.New()
	courseID := uuid.New()
	authorizedMux := func(roles []auth.Role) *http.ServeMux {
		repo := &fakeRepository{
			listFn:         func(context.Context, ListParams) ([]Record, error) { return nil, nil },
			countFn:        func(context.Context, ListFilter) (int64, error) { return 0, nil },
			createFn:       func(context.Context, CreateParams) (Record, error) { return sampleCourse(courseID), nil },
			byIDFn:         func(context.Context, uuid.UUID) (Record, error) { return sampleCourse(courseID), nil },
			updateFn:       func(context.Context, UpdateParams) (Record, error) { return sampleCourse(courseID), nil },
			updateStatusFn: func(context.Context, uuid.UUID, CourseStatus) (Record, error) { return sampleCourse(courseID), nil },
		}
		set := middlewareutil.NewSet(func(next http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				next(w, r.WithContext(auth.ContextWithUserID(r.Context(), actorID)))
			}
		})
		mux := http.NewServeMux()
		roleQuerier := &fakeCourseRoleQuerier{roles: roles}
		access := &fakeStudentCourseAccessChecker{allowed: true}
		newCourseHandlerWithAuthorization(repo, roleQuerier, access).RegisterRoutes(mux, set, auth.NewAuthorizer(roleQuerier, nil))
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
		{name: "student cannot list", roles: []auth.Role{auth.STUDENT}, method: http.MethodGet, path: "/api/courses", wantCode: http.StatusForbidden},
		{name: "student get reaches conditional access logic", roles: []auth.Role{auth.STUDENT}, method: http.MethodGet, path: "/api/courses/" + courseID.String(), wantCode: http.StatusOK},
		{name: "experimenter can list", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodGet, path: "/api/courses", wantCode: http.StatusOK},
		{name: "experimenter can create", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodPost, path: "/api/courses", body: `{"code":"BIO101","title":"Biology"}`, wantCode: http.StatusCreated},
		{name: "experimenter can get", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodGet, path: "/api/courses/" + courseID.String(), wantCode: http.StatusOK},
		{name: "admin can update", roles: []auth.Role{auth.ADMIN}, method: http.MethodPut, path: "/api/courses/" + courseID.String(), body: `{"code":"BIO102","title":"Advanced Biology"}`, wantCode: http.StatusOK},
		{name: "admin can update status", roles: []auth.Role{auth.ADMIN}, method: http.MethodPut, path: "/api/courses/" + courseID.String() + "/status", body: `{"status":"PUBLISHED"}`, wantCode: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			authorizedMux(tt.roles).ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			assert.Equal(t, tt.wantCode, recorder.Code)
		})
	}
}
