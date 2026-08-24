//go:build integration

package course

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"sciedu-backend/internal/auth"
	"sciedu-backend/internal/experiment"

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type courseIntegrationRoleQuerier struct {
	roles []auth.Role
}

func (q courseIntegrationRoleQuerier) ActiveUserRoles(context.Context, uuid.UUID) ([]auth.Role, error) {
	return q.roles, nil
}

func newCourseAPI(t *testing.T, pool *pgxpool.Pool, actorID uuid.UUID, roles ...auth.Role) *http.ServeMux {
	t.Helper()

	roleQuerier := courseIntegrationRoleQuerier{roles: roles}
	service := NewService(NewStore(pool), roleQuerier, experiment.NewStore(pool), zap.NewNop())
	handler := NewHandler(service, zap.NewNop())
	set := middlewareutil.NewSet(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(auth.ContextWithUserID(r.Context(), actorID)))
		}
	})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, set, auth.NewAuthorizer(roleQuerier, nil))
	return mux
}

func callCourseAPI(t *testing.T, mux *http.ServeMux, method, target, body string) (int, []byte) {
	t.Helper()

	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.Bytes()
}

func decodeCourseResponse(t *testing.T, body []byte) courseResponse {
	t.Helper()

	var response courseResponse
	require.NoError(t, json.Unmarshal(body, &response), "response body: %s", body)
	return response
}

func seedCourseAPIStudentAccess(t *testing.T, pool *pgxpool.Pool, courseID uuid.UUID) uuid.UUID {
	t.Helper()

	studentID := uuid.New()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, name, roles)
		VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[])
	`, studentID, "course-api-"+studentID.String()+"@example.test", "Course API student")
	require.NoError(t, err)

	configuration := `{"maxAttempts":1,"allowRetry":false,"showScore":true,"showExplanations":false,"gradingMode":"AUTOMATIC","correctAnswerReleaseMode":"NEVER"}`
	var experimentID uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO experiments (
			created_by, name, configuration, status, scheduled_start_at, scheduled_end_at
		) VALUES ($1, $2, $3::jsonb, 'ACTIVE', $4, $5)
		RETURNING id
	`, studentID, "Course API access", configuration, time.Now().Add(-time.Hour), time.Now().Add(time.Hour)).Scan(&experimentID))
	_, err = pool.Exec(t.Context(), `
		INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)
	`, experimentID, studentID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		INSERT INTO experiment_courses (experiment_id, course_id) VALUES ($1, $2)
	`, experimentID, courseID)
	require.NoError(t, err)

	t.Cleanup(func() {
		//nolint:errcheck // cascades remove participant and Course links
		pool.Exec(context.Background(), "DELETE FROM experiments WHERE id = $1", experimentID)
		//nolint:errcheck // best-effort cleanup for an isolated integration fixture
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", studentID)
	})
	return studentID
}

func TestAPICourseLifecycleAndList(t *testing.T) {
	_, pool, prefix := newIntegrationStore(t)
	manager := newCourseAPI(t, pool, uuid.New(), auth.EXPERIMENTER)

	code, body := callCourseAPI(t, manager, http.MethodPost, "/api/courses", fmt.Sprintf(
		`{"code":%q,"title":%q,"description":"initial description"}`, prefix+"BIO101", prefix+"Biology",
	))
	require.Equal(t, http.StatusCreated, code, "response body: %s", body)
	first := decodeCourseResponse(t, body)
	assert.Equal(t, CourseStatusDRAFT, first.Status)
	assert.Equal(t, "initial description", *first.Description)

	code, body = callCourseAPI(t, manager, http.MethodPost, "/api/courses", fmt.Sprintf(
		`{"code":%q,"title":%q}`, prefix+"CHEM101", prefix+"Chemistry",
	))
	require.Equal(t, http.StatusCreated, code, "response body: %s", body)
	second := decodeCourseResponse(t, body)

	code, body = callCourseAPI(t, manager, http.MethodPut, "/api/courses/"+first.ID.String(), fmt.Sprintf(
		`{"code":%q,"title":%q,"description":"updated description"}`, prefix+"BIO102", prefix+"Advanced Biology",
	))
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	updated := decodeCourseResponse(t, body)
	assert.Equal(t, prefix+"BIO102", updated.Code)
	assert.Equal(t, prefix+"Advanced Biology", updated.Title)
	assert.Equal(t, "updated description", *updated.Description)
	assert.Equal(t, CourseStatusDRAFT, updated.Status)

	code, body = callCourseAPI(t, manager, http.MethodPut, "/api/courses/"+first.ID.String()+"/status", `{"status":"PUBLISHED"}`)
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	published := decodeCourseResponse(t, body)
	assert.Equal(t, CourseStatusPUBLISHED, published.Status)

	code, body = callCourseAPI(t, manager, http.MethodGet, "/api/courses/"+first.ID.String(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	fetched := decodeCourseResponse(t, body)
	assert.Equal(t, published, fetched)

	query := url.Values{"search": {prefix}, "page": {"1"}, "pageSize": {"1"}}
	code, body = callCourseAPI(t, manager, http.MethodGet, "/api/courses?"+query.Encode(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	var page paginatedCoursesResponse
	require.NoError(t, json.Unmarshal(body, &page))
	assert.Len(t, page.Items, 1)
	assert.Equal(t, int32(2), page.TotalItems)
	assert.Equal(t, int32(2), page.TotalPages)
	assert.Equal(t, int32(1), page.CurrentPage)
	assert.Equal(t, int32(1), page.PageSize)
	assert.True(t, page.HasNextPage)

	query = url.Values{"search": {prefix}, "status": {string(CourseStatusPUBLISHED)}}
	code, body = callCourseAPI(t, manager, http.MethodGet, "/api/courses?"+query.Encode(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, first.ID, page.Items[0].ID)

	code, body = callCourseAPI(t, manager, http.MethodPost, "/api/courses", fmt.Sprintf(
		`{"code":%q,"title":"Duplicate"}`, strings.ToUpper(prefix+"BIO102"),
	))
	assert.Equal(t, http.StatusConflict, code, "response body: %s", body)

	code, body = callCourseAPI(t, manager, http.MethodPut, "/api/courses/"+second.ID.String(), fmt.Sprintf(
		`{"code":%q,"title":"Duplicate update"}`, strings.ToUpper(prefix+"BIO102"),
	))
	assert.Equal(t, http.StatusConflict, code, "response body: %s", body)

	studentID := seedCourseAPIStudentAccess(t, pool, first.ID)
	student := newCourseAPI(t, pool, studentID, auth.STUDENT)
	code, body = callCourseAPI(t, student, http.MethodGet, "/api/courses/"+first.ID.String(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	assert.Equal(t, first.ID, decodeCourseResponse(t, body).ID)

	code, body = callCourseAPI(t, student, http.MethodGet, "/api/courses/"+second.ID.String(), "")
	assert.Equal(t, http.StatusForbidden, code, "response body: %s", body)
}

func TestAPICourseMissingResourcesReturnNotFound(t *testing.T) {
	_, pool, prefix := newIntegrationStore(t)
	manager := newCourseAPI(t, pool, uuid.New(), auth.ADMIN)
	missingID := uuid.New()

	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "get", method: http.MethodGet, target: "/api/courses/" + missingID.String()},
		{
			name: "update", method: http.MethodPut, target: "/api/courses/" + missingID.String(),
			body: fmt.Sprintf(`{"code":%q,"title":"Missing"}`, prefix+"MISSING"),
		},
		{
			name: "update status", method: http.MethodPut, target: "/api/courses/" + missingID.String() + "/status",
			body: `{"status":"ARCHIVED"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := callCourseAPI(t, manager, tt.method, tt.target, tt.body)
			assert.Equal(t, http.StatusNotFound, code, fmt.Sprintf("response body: %s", body))
		})
	}
}
