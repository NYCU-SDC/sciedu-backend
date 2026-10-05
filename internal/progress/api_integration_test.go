//go:build integration

package progress

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"sciedu-backend/internal/auth"
	"sciedu-backend/internal/course"
	"sciedu-backend/internal/experiment"

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type progressApiFixture struct {
	actorID     uuid.UUID
	actorRoles  []auth.Role
	studentA    uuid.UUID
	studentB    uuid.UUID
	experiment  uuid.UUID
	course      uuid.UUID
	pageA       uuid.UUID
	pageB       uuid.UUID
	assignedAtA time.Time
	assignedAtB time.Time
}

type progressIntegrationRoleQuerier struct {
	rolesByUser map[uuid.UUID][]auth.Role
}

func (q progressIntegrationRoleQuerier) ActiveUserRoles(_ context.Context, userID uuid.UUID) ([]auth.Role, error) {
	roles, ok := q.rolesByUser[userID]
	if !ok {
		return nil, errors.New("no roles configured for user")
	}
	return roles, nil
}

type progressApiCourseAccess struct {
	store *course.Store
}

func (a *progressApiCourseAccess) CheckStudentCourseAccess(ctx context.Context, studentID, courseID uuid.UUID) (CourseAccessDecision, error) {
	decision, err := a.store.CourseForStudent(ctx, studentID, courseID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CourseAccessDecision{Found: false}, nil
		}
		return CourseAccessDecision{}, err
	}
	return CourseAccessDecision{Found: true, Allowed: decision.Allowed}, nil
}

func newProgressIntegrationAPI(t *testing.T, pool *pgxpool.Pool, actingAs uuid.UUID, rolesByUser map[uuid.UUID][]auth.Role) *http.ServeMux {
	t.Helper()

	roles := progressIntegrationRoleQuerier{rolesByUser: rolesByUser}
	set := middlewareutil.NewSet(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(auth.ContextWithUserID(r.Context(), actingAs)))
		}
	})
	store := NewStore(pool)
	service := NewService(store, experiment.NewStore(pool), &progressApiCourseAccess{course.NewStore(pool)}, nil, zap.NewNop())
	handler := NewHandler(service, zap.NewNop())
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, set, auth.NewAuthorizer(roles, nil))
	return mux
}

func newProgressApiPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("PAGE_PROGRESS_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PAGE_PROGRESS_INTEGRATION_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err, "create integration pool")
	t.Cleanup(pool.Close)
	require.NoError(t, pool.Ping(t.Context()), "reach integration database")
	return pool
}

func seedProgressApiFixture(t *testing.T, pool *pgxpool.Pool) progressApiFixture {
	t.Helper()

	suffix := uuid.NewString()
	ctx := t.Context()
	fx := progressApiFixture{
		actorID:    uuid.New(),
		studentA:   uuid.New(),
		studentB:   uuid.New(),
		actorRoles: []auth.Role{auth.EXPERIMENTER},
	}

	for i, u := range []struct {
		id    uuid.UUID
		email string
		role  string
	}{
		{fx.actorID, "progress-api-actor-" + suffix + "@example.test", "EXPERIMENTER"},
		{fx.studentA, "progress-api-sa-" + suffix + "@example.test", "STUDENT"},
		{fx.studentB, "progress-api-sb-" + suffix + "@example.test", "STUDENT"},
	} {
		_, err := pool.Exec(ctx,
			"INSERT INTO users (id, email, name, roles) VALUES ($1, $2, $3, ARRAY[$4]::user_role[])",
			u.id, u.email, fmt.Sprintf("Progress API user %d", i), u.role)
		require.NoError(t, err, "seed user %d", i)
	}

	require.NoError(t, pool.QueryRow(ctx,
		"INSERT INTO courses (code, title, status) VALUES ($1, $2, 'PUBLISHED') RETURNING id",
		"PROG-"+suffix, "Progress API Course").Scan(&fx.course), "seed course")
	require.NoError(t, pool.QueryRow(ctx,
		"INSERT INTO pages (course_id, title, display_order) VALUES ($1, $2, 0) RETURNING id",
		fx.course, "P1").Scan(&fx.pageA), "seed pageA")
	require.NoError(t, pool.QueryRow(ctx,
		"INSERT INTO pages (course_id, title, display_order) VALUES ($1, $2, 1) RETURNING id",
		fx.course, "P2").Scan(&fx.pageB), "seed pageB")

	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(24 * time.Hour)
	configuration := `{
		"maxAttempts": 1,
		"allowRetry": false,
		"showScore": true,
		"showExplanations": true,
		"gradingMode": "AUTOMATIC",
		"correctAnswerReleaseMode": "AFTER_PAGE_SUBMISSION"
	}`
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO experiments (created_by, name, configuration, status, scheduled_start_at, scheduled_end_at)
		VALUES ($1, $2, $3::jsonb, 'ACTIVE', $4, $5)
		RETURNING id`,
		fx.actorID, "Progress API Exp "+suffix, configuration, start, end).Scan(&fx.experiment), "seed experiment")

	_, err := pool.Exec(ctx,
		"INSERT INTO experiment_courses (experiment_id, course_id) VALUES ($1, $2)",
		fx.experiment, fx.course)
	require.NoError(t, err, "assign course to experiment")

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO experiment_participants (experiment_id, user_id)
		VALUES ($1, $2) RETURNING assigned_at`,
		fx.experiment, fx.studentA).Scan(&fx.assignedAtA), "assign studentA")
	// Delay by a microsecond so sort by assigned_at is deterministic.
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO experiment_participants (experiment_id, user_id)
		VALUES ($1, $2) RETURNING assigned_at`,
		fx.experiment, fx.studentB).Scan(&fx.assignedAtB), "assign studentB")

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		//nolint:errcheck
		pool.Exec(cleanupCtx, "DELETE FROM experiments WHERE id = $1", fx.experiment)
		//nolint:errcheck
		pool.Exec(cleanupCtx, "DELETE FROM pages WHERE id = ANY($1::uuid[])", []uuid.UUID{fx.pageA, fx.pageB})
		//nolint:errcheck
		pool.Exec(cleanupCtx, "DELETE FROM courses WHERE id = $1", fx.course)
		//nolint:errcheck
		pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = ANY($1::uuid[])",
			[]uuid.UUID{fx.actorID, fx.studentA, fx.studentB})
	})

	return fx
}

func callProgressAPI(t *testing.T, mux *http.ServeMux, method, target string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr.Code, rr.Body.Bytes()
}

func TestAPIProgressStudentHappyPath(t *testing.T) {
	pool := newProgressApiPool(t)
	fx := seedProgressApiFixture(t, pool)

	rolesByUser := map[uuid.UUID][]auth.Role{
		fx.studentA: {auth.STUDENT},
		fx.studentB: {auth.STUDENT},
	}
	mux := newProgressIntegrationAPI(t, pool, fx.studentA, rolesByUser)

	// GET /me returns NOT_STARTED
	code, body := callProgressAPI(t, mux, http.MethodGet, "/api/progress/me?courseId="+fx.course.String())
	require.Equal(t, http.StatusOK, code, "body: %s", body)
	detail := decodeDetail(t, body)
	assert.Equal(t, StatusNotStarted, detail.Status)
	assert.Equal(t, int32(2), detail.TotalPageCount)
	assert.Equal(t, fx.experiment, detail.ExperimentID)

	// Reach page A
	code, body = callProgressAPI(t, mux, http.MethodPut, "/api/progress/me/pages/"+fx.pageA.String()+"/reach")
	require.Equal(t, http.StatusOK, code, "body: %s", body)
	detail = decodeDetail(t, body)
	assert.Equal(t, StatusInProgress, detail.Status)
	assert.Equal(t, int32(1), detail.ReachedPageCount)
	assert.True(t, detail.Pages[0].Reached)
	assert.False(t, detail.Pages[0].Completed)
	require.NotNil(t, detail.HighestReachedPage)
	assert.Equal(t, fx.pageA, detail.HighestReachedPage.PageID)

	// Complete page A
	code, body = callProgressAPI(t, mux, http.MethodPut, "/api/progress/me/pages/"+fx.pageA.String()+"/completion")
	require.Equal(t, http.StatusOK, code, "body: %s", body)
	detail = decodeDetail(t, body)
	assert.Equal(t, StatusInProgress, detail.Status)
	assert.Equal(t, int32(1), detail.CompletedPageCount)
	assert.True(t, detail.Pages[0].Completed)

	// Complete page B → COMPLETED
	code, body = callProgressAPI(t, mux, http.MethodPut, "/api/progress/me/pages/"+fx.pageB.String()+"/completion")
	require.Equal(t, http.StatusOK, code, "body: %s", body)
	detail = decodeDetail(t, body)
	assert.Equal(t, StatusCompleted, detail.Status)
	assert.Equal(t, int32(100), detail.CompletionPercentage)
}

func TestAPIProgressManagementListAndDetail(t *testing.T) {
	pool := newProgressApiPool(t)
	fx := seedProgressApiFixture(t, pool)

	rolesByUser := map[uuid.UUID][]auth.Role{
		fx.actorID:  {auth.EXPERIMENTER},
		fx.studentA: {auth.STUDENT},
	}

	// Have studentA reach one page, studentB untouched.
	studentMux := newProgressIntegrationAPI(t, pool, fx.studentA, rolesByUser)
	code, _ := callProgressAPI(t, studentMux, http.MethodPut, "/api/progress/me/pages/"+fx.pageA.String()+"/reach")
	require.Equal(t, http.StatusOK, code)

	// Act as EXPERIMENTER for mgmt endpoints.
	mgmtMux := newProgressIntegrationAPI(t, pool, fx.actorID, rolesByUser)

	code, body := callProgressAPI(t, mgmtMux, http.MethodGet, fmt.Sprintf("/api/progress/students?experimentId=%s&courseId=%s", fx.experiment, fx.course))
	require.Equal(t, http.StatusOK, code, "body: %s", body)

	var listResp paginatedStudentProgressResponse
	require.NoError(t, json.Unmarshal(body, &listResp))
	require.Len(t, listResp.Items, 2, "both participants listed, including untouched studentB")
	assert.Equal(t, fx.studentA, listResp.Items[0].Student.ID, "assignedAt order puts A first")
	assert.Equal(t, StatusInProgress, listResp.Items[0].Progress.Status)
	assert.Equal(t, fx.studentB, listResp.Items[1].Student.ID)
	assert.Equal(t, StatusNotStarted, listResp.Items[1].Progress.Status)

	code, body = callProgressAPI(t, mgmtMux, http.MethodGet, fmt.Sprintf("/api/progress/students/%s?experimentId=%s&courseId=%s", fx.studentA, fx.experiment, fx.course))
	require.Equal(t, http.StatusOK, code, "body: %s", body)
	detail := decodeDetail(t, body)
	assert.Equal(t, fx.studentA, detail.StudentID)
	assert.Equal(t, fx.experiment, detail.ExperimentID)
	assert.Equal(t, StatusInProgress, detail.Status)
	assert.Equal(t, int32(1), detail.ReachedPageCount)
}

func TestAPIProgressAuthFailures(t *testing.T) {
	pool := newProgressApiPool(t)
	fx := seedProgressApiFixture(t, pool)

	rolesByUser := map[uuid.UUID][]auth.Role{
		fx.studentA: {auth.STUDENT},
	}
	mux := newProgressIntegrationAPI(t, pool, fx.studentA, rolesByUser)

	// Unknown course → 404 (course does not exist in DB)
	code, _ := callProgressAPI(t, mux, http.MethodGet, "/api/progress/me?courseId="+uuid.NewString())
	assert.Equal(t, http.StatusNotFound, code)

	// Student not assigned to any experiment → 404
	loneStudent := uuid.New()
	_, err := pool.Exec(t.Context(),
		"INSERT INTO users (id, email, name, roles) VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[])",
		loneStudent, "progress-lone-"+loneStudent.String()+"@example.test", "Lone student")
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", loneStudent)
	})
	loneMux := newProgressIntegrationAPI(t, pool, loneStudent, map[uuid.UUID][]auth.Role{loneStudent: {auth.STUDENT}})
	code, _ = callProgressAPI(t, loneMux, http.MethodGet, "/api/progress/me?courseId="+fx.course.String())
	assert.Equal(t, http.StatusNotFound, code)

	// Draft course + student in current exp → 403
	draftCourseID := uuid.New()
	require.NoError(t, pool.QueryRow(t.Context(),
		"INSERT INTO courses (code, title, status) VALUES ($1, $2, 'DRAFT') RETURNING id",
		"DRAFT-"+uuid.NewString(), "Draft Course").Scan(&draftCourseID))
	_, err = pool.Exec(t.Context(),
		"INSERT INTO experiment_courses (experiment_id, course_id) VALUES ($1, $2)",
		fx.experiment, draftCourseID)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck
		pool.Exec(context.Background(), "DELETE FROM courses WHERE id = $1", draftCourseID)
	})

	code, _ = callProgressAPI(t, mux, http.MethodGet, "/api/progress/me?courseId="+draftCourseID.String())
	assert.Equal(t, http.StatusForbidden, code)
}

func decodeDetail(t *testing.T, body []byte) courseProgressDetailResponse {
	t.Helper()
	var detail courseProgressDetailResponse
	require.NoError(t, json.Unmarshal(body, &detail), "body: %s", body)
	return detail
}
