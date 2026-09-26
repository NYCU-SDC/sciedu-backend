//go:build integration

package experiment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"sciedu-backend/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedAssignmentCourse(t *testing.T, pool *pgxpool.Pool, status CourseStatus) uuid.UUID {
	t.Helper()

	var courseID uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO courses (code, title, description, status)
		VALUES ($1, $2, $3, $4::course_status)
		RETURNING id
	`, "assignment-"+uuid.NewString(), "Assignment course", "Course assignment fixture", status).Scan(&courseID))
	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup for an isolated integration fixture
		pool.Exec(context.Background(), "DELETE FROM courses WHERE id = $1", courseID)
	})
	return courseID
}

func courseBatchBody(t *testing.T, courseIDs ...uuid.UUID) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"courseIds": courseIDs})
	require.NoError(t, err)
	return string(body)
}

func countCourseAssignments(t *testing.T, pool *pgxpool.Pool, experimentID uuid.UUID) int {
	t.Helper()
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM experiment_courses WHERE experiment_id = $1
	`, experimentID).Scan(&count))
	return count
}

func TestAPICourseAssignmentLifecycle(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	start := time.Now().UTC().Add(-time.Hour)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}

	tests := []struct {
		status         Status
		wantAddCode    int
		wantDeleteCode int
	}{
		{status: StatusDraft, wantAddCode: http.StatusOK, wantDeleteCode: http.StatusNoContent},
		{status: StatusScheduled, wantAddCode: http.StatusOK, wantDeleteCode: http.StatusNoContent},
		{status: StatusActive, wantAddCode: http.StatusOK, wantDeleteCode: http.StatusConflict},
		{status: StatusCompleted, wantAddCode: http.StatusConflict, wantDeleteCode: http.StatusConflict},
		{status: StatusArchived, wantAddCode: http.StatusConflict, wantDeleteCode: http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			experimentID := seedExperimentForUpdate(t, pool, actorID, tt.status, "Course lifecycle "+string(tt.status), start, start.Add(2*time.Hour), configuration)
			courseID := seedAssignmentCourse(t, pool, CourseStatusDRAFT)
			code, body := callExperimentAPI(t, mux, http.MethodGet, "/api/experiments/"+experimentID.String()+"/courses", "")
			require.Equal(t, http.StatusOK, code, "management list must remain available in %s: %s", tt.status, body)

			code, body = callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+experimentID.String()+"/courses", courseBatchBody(t, courseID))
			require.Equal(t, tt.wantAddCode, code, "response body: %s", body)
			if tt.wantAddCode == http.StatusOK {
				var assignments []experimentCourseAssignmentResponse
				require.NoError(t, json.Unmarshal(body, &assignments))
				require.Len(t, assignments, 1)
				assert.Equal(t, courseID, assignments[0].Course.ID)
				assert.Equal(t, CourseStatusDRAFT, assignments[0].Course.Status)
				assert.Equal(t, 1, countCourseAssignments(t, pool, experimentID))
			} else {
				assert.Zero(t, countCourseAssignments(t, pool, experimentID))
			}

			code, body = callExperimentAPI(t, mux, http.MethodDelete, "/api/experiments/"+experimentID.String()+"/courses/"+courseID.String(), "")
			require.Equal(t, tt.wantDeleteCode, code, "response body: %s", body)
			if tt.wantDeleteCode == http.StatusNoContent {
				assert.Zero(t, countCourseAssignments(t, pool, experimentID))
			}
		})
	}

	draftID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Idempotent Course removal", start, start.Add(time.Hour), configuration)
	missingCourseID := uuid.New()
	code, body := callExperimentAPI(t, mux, http.MethodDelete, "/api/experiments/"+draftID.String()+"/courses/"+missingCourseID.String(), "")
	require.Equal(t, http.StatusNoContent, code, "response body: %s", body)

	missingExperimentID := uuid.New()
	code, body = callExperimentAPI(t, mux, http.MethodDelete, "/api/experiments/"+missingExperimentID.String()+"/courses/"+missingCourseID.String(), "")
	assert.Equal(t, http.StatusNotFound, code, "response body: %s", body)
}

func TestAPICourseEligibilityAndAtomicBatch(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	start := time.Now().UTC().Add(3 * time.Hour)

	tests := []struct {
		name     string
		status   CourseStatus
		missing  bool
		wantCode int
	}{
		{name: "draft Course", status: CourseStatusDRAFT, wantCode: http.StatusOK},
		{name: "published Course", status: CourseStatusPUBLISHED, wantCode: http.StatusOK},
		{name: "archived Course", status: CourseStatusARCHIVED, wantCode: http.StatusConflict},
		{name: "missing Course", missing: true, wantCode: http.StatusConflict},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			experimentID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Course eligibility "+tt.name, start.Add(time.Duration(index)*time.Hour), start.Add(time.Duration(index+1)*time.Hour), configuration)
			courseID := uuid.New()
			if !tt.missing {
				courseID = seedAssignmentCourse(t, pool, tt.status)
			}
			code, body := callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+experimentID.String()+"/courses", courseBatchBody(t, courseID))
			require.Equal(t, tt.wantCode, code, "response body: %s", body)
			if tt.wantCode == http.StatusOK {
				assert.Equal(t, 1, countCourseAssignments(t, pool, experimentID))
			} else {
				assert.Zero(t, countCourseAssignments(t, pool, experimentID))
			}
		})
	}

	experimentID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Atomic Course batch", start.Add(10*time.Hour), start.Add(11*time.Hour), configuration)
	validID := seedAssignmentCourse(t, pool, CourseStatusDRAFT)
	archivedID := seedAssignmentCourse(t, pool, CourseStatusARCHIVED)
	code, body := callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+experimentID.String()+"/courses", courseBatchBody(t, validID, archivedID))
	require.Equal(t, http.StatusConflict, code, "response body: %s", body)
	assert.Zero(t, countCourseAssignments(t, pool, experimentID))

	code, body = callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+experimentID.String()+"/courses", courseBatchBody(t, validID, validID))
	require.Equal(t, http.StatusConflict, code, "response body: %s", body)
	assert.Zero(t, countCourseAssignments(t, pool, experimentID))

	code, body = callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+experimentID.String()+"/courses", courseBatchBody(t, validID))
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	newID := seedAssignmentCourse(t, pool, CourseStatusPUBLISHED)
	code, body = callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+experimentID.String()+"/courses", courseBatchBody(t, validID, newID))
	require.Equal(t, http.StatusConflict, code, "response body: %s", body)
	assert.Equal(t, 1, countCourseAssignments(t, pool, experimentID), "the new Course must roll back with the conflicting existing assignment")
}

func TestAPICourseListManagementAndStudentVisibility(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	studentID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	managementMux := newExperimentAPI(t, pool, actorID)
	studentMux := newExperimentAPIWithRoles(t, pool, studentID, []auth.Role{auth.STUDENT})
	multiRoleMux := newExperimentAPIWithRoles(t, pool, studentID, []auth.Role{auth.STUDENT, auth.EXPERIMENTER})
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	now := time.Now().UTC()
	experimentID := seedExperimentForUpdate(t, pool, actorID, StatusActive, "Visible Course list", now.Add(-time.Hour), now.Add(time.Hour), configuration)
	publishedID := seedAssignmentCourse(t, pool, CourseStatusPUBLISHED)
	archivedID := seedAssignmentCourse(t, pool, CourseStatusDRAFT)
	firstLinkedAt := now.Add(-2 * time.Minute)
	secondLinkedAt := now.Add(-time.Minute)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)
	`, experimentID, studentID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		INSERT INTO experiment_courses (experiment_id, course_id, linked_at)
		VALUES ($1, $2, $4), ($1, $3, $5)
	`, experimentID, publishedID, archivedID, firstLinkedAt, secondLinkedAt)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE courses SET status = 'ARCHIVED' WHERE id = $1`, archivedID)
	require.NoError(t, err)

	query := url.Values{"page": {"1"}, "pageSize": {"1"}}
	code, body := callExperimentAPI(t, managementMux, http.MethodGet, "/api/experiments/"+experimentID.String()+"/courses?"+query.Encode(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	var page paginatedExperimentCoursesResponse
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, archivedID, page.Items[0].Course.ID, "management ordering is linkedAt descending")
	assert.Equal(t, CourseStatusARCHIVED, page.Items[0].Course.Status)
	assert.Equal(t, int32(2), page.TotalItems)
	assert.Equal(t, int32(2), page.TotalPages)
	assert.True(t, page.HasNextPage)

	code, body = callExperimentAPI(t, studentMux, http.MethodGet, "/api/experiments/"+experimentID.String()+"/courses", "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, publishedID, page.Items[0].Course.ID)
	assert.Equal(t, int32(1), page.TotalItems, "student count uses the same PUBLISHED filter as the list")

	code, body = callExperimentAPI(t, multiRoleMux, http.MethodGet, "/api/experiments/"+experimentID.String()+"/courses", "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	require.NoError(t, json.Unmarshal(body, &page))
	assert.Len(t, page.Items, 2, "management visibility wins for a multi-role actor")

	code, body = callExperimentAPI(t, managementMux, http.MethodGet, "/api/experiments/"+experimentID.String(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	assert.Equal(t, int32(2), decodeExperimentDetail(t, body).CourseCount)
}

func TestAPIStudentCourseListAccessWindow(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	studentID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	mux := newExperimentAPIWithRoles(t, pool, studentID, []auth.Role{auth.STUDENT})
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	now := time.Now().UTC()
	tests := []struct {
		name        string
		status      Status
		start       time.Time
		end         time.Time
		participant bool
		wantCode    int
	}{
		{name: "active current participant", status: StatusActive, start: now.Add(-time.Hour), end: now.Add(time.Hour), participant: true, wantCode: http.StatusOK},
		{name: "not a participant", status: StatusActive, start: now.Add(-time.Hour), end: now.Add(time.Hour), wantCode: http.StatusForbidden},
		{name: "draft is inaccessible", status: StatusDraft, start: now.Add(-time.Hour), end: now.Add(time.Hour), participant: true, wantCode: http.StatusForbidden},
		{name: "scheduled is inaccessible", status: StatusScheduled, start: now.Add(-time.Hour), end: now.Add(time.Hour), participant: true, wantCode: http.StatusForbidden},
		{name: "completed is inaccessible", status: StatusCompleted, start: now.Add(-time.Hour), end: now.Add(time.Hour), participant: true, wantCode: http.StatusForbidden},
		{name: "archived is inaccessible", status: StatusArchived, start: now.Add(-time.Hour), end: now.Add(time.Hour), participant: true, wantCode: http.StatusForbidden},
		{name: "future active is inaccessible", status: StatusActive, start: now.Add(time.Hour), end: now.Add(2 * time.Hour), participant: true, wantCode: http.StatusForbidden},
		{name: "end boundary is exclusive", status: StatusActive, start: now.Add(-time.Hour), end: now, participant: true, wantCode: http.StatusForbidden},
		{name: "expired active is inaccessible", status: StatusActive, start: now.Add(-2 * time.Hour), end: now.Add(-time.Hour), participant: true, wantCode: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			experimentID := seedExperimentForUpdate(t, pool, actorID, tt.status, "Student access "+tt.name, tt.start, tt.end, configuration)
			courseID := seedAssignmentCourse(t, pool, CourseStatusPUBLISHED)
			_, err := pool.Exec(t.Context(), `INSERT INTO experiment_courses (experiment_id, course_id) VALUES ($1, $2)`, experimentID, courseID)
			require.NoError(t, err)
			if tt.participant {
				_, err = pool.Exec(t.Context(), `INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)`, experimentID, studentID)
				require.NoError(t, err)
			}

			code, body := callExperimentAPI(t, mux, http.MethodGet, "/api/experiments/"+experimentID.String()+"/courses", "")
			assert.Equal(t, tt.wantCode, code, "response body: %s", body)
		})
	}

	code, body := callExperimentAPI(t, mux, http.MethodGet, "/api/experiments/"+uuid.NewString()+"/courses", "")
	assert.Equal(t, http.StatusNotFound, code, "response body: %s", body)
}
