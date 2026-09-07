//go:build integration

package experiment

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

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type experimentIntegrationRoleQuerier struct {
	roles []auth.Role
}

func (q experimentIntegrationRoleQuerier) ActiveUserRoles(context.Context, uuid.UUID) ([]auth.Role, error) {
	return q.roles, nil
}

func newExperimentAPI(t *testing.T, pool *pgxpool.Pool, actorID uuid.UUID) *http.ServeMux {
	t.Helper()

	roles := experimentIntegrationRoleQuerier{roles: []auth.Role{auth.EXPERIMENTER}}
	set := middlewareutil.NewSet(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(auth.ContextWithUserID(r.Context(), actorID)))
		}
	})
	handler := NewHandler(NewService(NewStore(pool), zap.NewNop()), zap.NewNop())
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, set, auth.NewAuthorizer(roles, nil))
	return mux
}

func callExperimentAPI(t *testing.T, mux *http.ServeMux, method, target, body string) (int, []byte) {
	t.Helper()

	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.Bytes()
}

func experimentRequestBody(t *testing.T, name, description string, start, end time.Time, configuration Configuration) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"name":             name,
		"description":      description,
		"scheduledStartAt": start,
		"scheduledEndAt":   end,
		"configuration":    configuration,
	})
	require.NoError(t, err)
	return string(body)
}

func decodeExperimentDetail(t *testing.T, body []byte) experimentDetailResponse {
	t.Helper()

	var response experimentDetailResponse
	require.NoError(t, json.Unmarshal(body, &response), "response body: %s", body)
	return response
}

func seedExperimentAPIActor(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	actorID := uuid.New()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, name, roles)
		VALUES ($1, $2, $3, ARRAY['EXPERIMENTER']::user_role[])
	`, actorID, "experiment-api-"+actorID.String()+"@example.test", "Experiment API actor")
	require.NoError(t, err)

	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup for an isolated integration fixture
		pool.Exec(context.Background(), "DELETE FROM experiments WHERE created_by = $1", actorID)
		//nolint:errcheck // best-effort cleanup for an isolated integration fixture
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", actorID)
	})
	return actorID
}

func addExperimentCounts(t *testing.T, pool *pgxpool.Pool, experimentID uuid.UUID) {
	t.Helper()

	participantID := uuid.New()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, name, roles)
		VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[])
	`, participantID, "experiment-participant-"+participantID.String()+"@example.test", "Experiment participant")
	require.NoError(t, err)

	var courseID uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO courses (code, title)
		VALUES ($1, $2)
		RETURNING id
	`, "experiment-api-"+uuid.NewString(), "Experiment API course").Scan(&courseID))

	_, err = pool.Exec(t.Context(), `
		INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)
	`, experimentID, participantID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		INSERT INTO experiment_courses (experiment_id, course_id) VALUES ($1, $2)
	`, experimentID, courseID)
	require.NoError(t, err)

	t.Cleanup(func() {
		//nolint:errcheck // experiment cleanup cascades assignment rows
		pool.Exec(context.Background(), "DELETE FROM courses WHERE id = $1", courseID)
		//nolint:errcheck // experiment cleanup cascades assignment rows
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", participantID)
	})
}

func seedExperimentForUpdate(
	t *testing.T,
	pool *pgxpool.Pool,
	actorID uuid.UUID,
	status Status,
	name string,
	start time.Time,
	end time.Time,
	configuration Configuration,
) uuid.UUID {
	t.Helper()

	configurationJSON, err := json.Marshal(configuration)
	require.NoError(t, err)
	var experimentID uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO experiments (
			created_by, name, description, configuration, status, scheduled_start_at, scheduled_end_at
		) VALUES ($1, $2, '', $3::jsonb, $4::experiment_status, $5, $6)
		RETURNING id
	`, actorID, name, configurationJSON, status, start, end).Scan(&experimentID))
	return experimentID
}

func TestAPIExperimentLifecycleAndList(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	prefix := "phase5-" + uuid.NewString()
	firstStart := time.Date(2026, time.September, 1, 1, 0, 0, 0, time.UTC)
	secondStart := time.Date(2026, time.October, 1, 1, 0, 0, 0, time.UTC)
	initialConfiguration := Configuration{
		MaxAttempts:              1,
		AllowRetry:               false,
		ShowScore:                true,
		ShowExplanations:         false,
		GradingMode:              GradingModeAutomatic,
		CorrectAnswerReleaseMode: CorrectAnswerReleaseNever,
	}

	code, body := callExperimentAPI(t, mux, http.MethodPost, "/api/experiments", experimentRequestBody(
		t, prefix+" Alpha", "initial description", firstStart, firstStart.Add(24*time.Hour), initialConfiguration,
	))
	require.Equal(t, http.StatusCreated, code, "response body: %s", body)
	created := decodeExperimentDetail(t, body)
	assert.Equal(t, actorID, created.CreatedBy)
	assert.Equal(t, StatusDraft, created.Status)
	assert.Equal(t, initialConfiguration, created.Configuration)
	assert.Equal(t, int32(0), created.ParticipantCount)
	assert.Equal(t, int32(0), created.CourseCount)

	code, body = callExperimentAPI(t, mux, http.MethodPost, "/api/experiments", experimentRequestBody(
		t, prefix+" Beta", "second description", secondStart, secondStart.Add(24*time.Hour), initialConfiguration,
	))
	require.Equal(t, http.StatusCreated, code, "response body: %s", body)
	second := decodeExperimentDetail(t, body)

	addExperimentCounts(t, pool, created.ID)

	code, body = callExperimentAPI(t, mux, http.MethodPut, "/api/experiments/"+created.ID.String()+"/status", `{"status":"SCHEDULED"}`)
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	statusUpdated := decodeExperimentDetail(t, body)
	assert.Equal(t, StatusScheduled, statusUpdated.Status)
	assert.Equal(t, int32(1), statusUpdated.ParticipantCount)
	assert.Equal(t, int32(1), statusUpdated.CourseCount)

	updatedConfiguration := Configuration{
		MaxAttempts:              3,
		AllowRetry:               true,
		ShowScore:                false,
		ShowExplanations:         true,
		GradingMode:              GradingModeManual,
		CorrectAnswerReleaseMode: CorrectAnswerReleaseAfterCourseCompletion,
	}
	updatedStart := firstStart.Add(2 * time.Hour)
	code, body = callExperimentAPI(t, mux, http.MethodPut, "/api/experiments/"+created.ID.String(), experimentRequestBody(
		t, prefix+" Alpha updated", "updated description", updatedStart, updatedStart.Add(48*time.Hour), updatedConfiguration,
	))
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	updated := decodeExperimentDetail(t, body)
	assert.Equal(t, prefix+" Alpha updated", updated.Name)
	assert.Equal(t, "updated description", *updated.Description)
	assert.Equal(t, updatedConfiguration, updated.Configuration)
	assert.Equal(t, StatusScheduled, updated.Status, "metadata update must preserve lifecycle status")
	assert.Equal(t, int32(1), updated.ParticipantCount)
	assert.Equal(t, int32(1), updated.CourseCount)

	code, body = callExperimentAPI(t, mux, http.MethodGet, "/api/experiments/"+created.ID.String(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	fetched := decodeExperimentDetail(t, body)
	assert.Equal(t, updated.Name, fetched.Name)
	assert.Equal(t, updated.Configuration, fetched.Configuration)
	assert.Equal(t, int32(1), fetched.ParticipantCount)
	assert.Equal(t, int32(1), fetched.CourseCount)

	query := url.Values{"search": {prefix}, "page": {"1"}, "pageSize": {"1"}}
	code, body = callExperimentAPI(t, mux, http.MethodGet, "/api/experiments?"+query.Encode(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	var page paginatedExperimentsResponse
	require.NoError(t, json.Unmarshal(body, &page))
	assert.Len(t, page.Items, 1)
	assert.Equal(t, int32(2), page.TotalItems)
	assert.Equal(t, int32(2), page.TotalPages)
	assert.Equal(t, int32(1), page.CurrentPage)
	assert.Equal(t, int32(1), page.PageSize)
	assert.True(t, page.HasNextPage)

	query = url.Values{"search": {prefix}, "status": {string(StatusScheduled)}}
	code, body = callExperimentAPI(t, mux, http.MethodGet, "/api/experiments?"+query.Encode(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, created.ID, page.Items[0].ID)

	boundary := time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	query = url.Values{"search": {prefix}, "scheduledFrom": {boundary}}
	code, body = callExperimentAPI(t, mux, http.MethodGet, "/api/experiments?"+query.Encode(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, second.ID, page.Items[0].ID)

	query = url.Values{"search": {prefix}, "scheduledTo": {boundary}}
	code, body = callExperimentAPI(t, mux, http.MethodGet, "/api/experiments?"+query.Encode(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, created.ID, page.Items[0].ID)
}

func TestAPIExperimentMissingResourcesReturnNotFound(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	missingID := uuid.New()
	start := time.Date(2026, time.September, 1, 1, 0, 0, 0, time.UTC)
	configuration := Configuration{
		MaxAttempts:              1,
		GradingMode:              GradingModeAutomatic,
		CorrectAnswerReleaseMode: CorrectAnswerReleaseNever,
	}

	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "get", method: http.MethodGet, target: "/api/experiments/" + missingID.String()},
		{
			name: "update", method: http.MethodPut, target: "/api/experiments/" + missingID.String(),
			body: experimentRequestBody(t, "Missing", "", start, start.Add(time.Hour), configuration),
		},
		{
			name: "update status", method: http.MethodPut, target: "/api/experiments/" + missingID.String() + "/status",
			body: `{"status":"ACTIVE"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := callExperimentAPI(t, mux, tt.method, tt.target, tt.body)
			assert.Equal(t, http.StatusNotFound, code, fmt.Sprintf("response body: %s", body))
		})
	}
}

func TestAPIExperimentUpdateLifecyclePolicy(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	start := time.Date(2026, time.September, 10, 9, 0, 0, 0, time.UTC)
	configuration := Configuration{
		MaxAttempts:              1,
		AllowRetry:               false,
		ShowScore:                true,
		ShowExplanations:         false,
		GradingMode:              GradingModeAutomatic,
		CorrectAnswerReleaseMode: CorrectAnswerReleaseNever,
	}

	tests := []struct {
		name           string
		status         Status
		requestName    string
		requestEnd     time.Time
		wantCode       int
		wantStoredName string
		wantStoredEnd  time.Time
	}{
		{name: "draft fully editable", status: StatusDraft, requestName: "Draft updated", requestEnd: start.Add(2 * time.Hour), wantCode: http.StatusOK, wantStoredName: "Draft updated", wantStoredEnd: start.Add(2 * time.Hour)},
		{name: "scheduled fully editable", status: StatusScheduled, requestName: "Scheduled updated", requestEnd: start.Add(2 * time.Hour), wantCode: http.StatusOK, wantStoredName: "Scheduled updated", wantStoredEnd: start.Add(2 * time.Hour)},
		{name: "active identical retry", status: StatusActive, requestName: "Original", requestEnd: start.Add(time.Hour), wantCode: http.StatusOK, wantStoredName: "Original", wantStoredEnd: start.Add(time.Hour)},
		{name: "active end extension", status: StatusActive, requestName: "Original", requestEnd: start.Add(2 * time.Hour), wantCode: http.StatusOK, wantStoredName: "Original", wantStoredEnd: start.Add(2 * time.Hour)},
		{name: "active name change rejected", status: StatusActive, requestName: "Changed", requestEnd: start.Add(time.Hour), wantCode: http.StatusConflict, wantStoredName: "Original", wantStoredEnd: start.Add(time.Hour)},
		{name: "active shorter end rejected", status: StatusActive, requestName: "Original", requestEnd: start.Add(30 * time.Minute), wantCode: http.StatusConflict, wantStoredName: "Original", wantStoredEnd: start.Add(time.Hour)},
		{name: "completed rejected", status: StatusCompleted, requestName: "Original", requestEnd: start.Add(time.Hour), wantCode: http.StatusConflict, wantStoredName: "Original", wantStoredEnd: start.Add(time.Hour)},
		{name: "archived rejected", status: StatusArchived, requestName: "Original", requestEnd: start.Add(time.Hour), wantCode: http.StatusConflict, wantStoredName: "Original", wantStoredEnd: start.Add(time.Hour)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			experimentID := seedExperimentForUpdate(t, pool, actorID, tt.status, "Original", start, start.Add(time.Hour), configuration)
			code, body := callExperimentAPI(t, mux, http.MethodPut, "/api/experiments/"+experimentID.String(), experimentRequestBody(
				t, tt.requestName, "", start, tt.requestEnd, configuration,
			))
			require.Equal(t, tt.wantCode, code, "response body: %s", body)

			var storedName string
			var storedEnd time.Time
			require.NoError(t, pool.QueryRow(t.Context(), `
				SELECT name, scheduled_end_at FROM experiments WHERE id = $1
			`, experimentID).Scan(&storedName, &storedEnd))
			assert.Equal(t, tt.wantStoredName, storedName)
			assert.True(t, tt.wantStoredEnd.Equal(storedEnd))
		})
	}
}

func TestAPIExperimentScheduleUpdateEnforcesParticipantOverlap(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	configuration := Configuration{
		MaxAttempts:              1,
		GradingMode:              GradingModeAutomatic,
		CorrectAnswerReleaseMode: CorrectAnswerReleaseNever,
	}
	participantID := uuid.New()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, name, roles)
		VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[])
	`, participantID, "schedule-overlap-"+participantID.String()+"@example.test", "Schedule overlap participant")
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup for an isolated integration fixture
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", participantID)
	})

	targetStart := time.Date(2026, time.September, 10, 8, 0, 0, 0, time.UTC)
	otherStart := time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)
	targetID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Target", targetStart, targetStart.Add(time.Hour), configuration)
	otherID := seedExperimentForUpdate(t, pool, actorID, StatusCompleted, "Reserved", otherStart, otherStart.Add(time.Hour), configuration)
	_, err = pool.Exec(t.Context(), `
		INSERT INTO experiment_participants (experiment_id, user_id)
		VALUES ($1, $3), ($2, $3)
	`, targetID, otherID, participantID)
	require.NoError(t, err)

	code, body := callExperimentAPI(t, mux, http.MethodPut, "/api/experiments/"+targetID.String(), experimentRequestBody(
		t, "Target", "", otherStart.Add(-30*time.Minute), otherStart.Add(30*time.Minute), configuration,
	))
	require.Equal(t, http.StatusConflict, code, "response body: %s", body)

	var storedStart, storedEnd time.Time
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT scheduled_start_at, scheduled_end_at FROM experiments WHERE id = $1
	`, targetID).Scan(&storedStart, &storedEnd))
	assert.True(t, targetStart.Equal(storedStart))
	assert.True(t, targetStart.Add(time.Hour).Equal(storedEnd))

	code, body = callExperimentAPI(t, mux, http.MethodPut, "/api/experiments/"+targetID.String(), experimentRequestBody(
		t, "Target", "", otherStart.Add(-time.Hour), otherStart, configuration,
	))
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
}
