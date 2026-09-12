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

func TestAPIParticipantLifecycle(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	participantIDs := seedParticipantUsers(t, pool, 2)
	start := time.Date(2026, time.September, 10, 1, 0, 0, 0, time.UTC)
	experimentID := createExperimentViaAPI(t, mux, "Participant lifecycle", start, start.Add(2*time.Hour))

	requestBody, err := json.Marshal(map[string]any{"userIds": participantIDs})
	require.NoError(t, err)
	code, body := callExperimentAPI(
		t,
		mux,
		http.MethodPost,
		"/api/experiments/"+experimentID.String()+"/participants",
		string(requestBody),
	)
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	var added []participantAssignmentResponse
	require.NoError(t, json.Unmarshal(body, &added))
	require.Len(t, added, 2)
	assert.Equal(t, participantIDs[0], added[0].Participant.ID)
	assert.Equal(t, participantIDs[1], added[1].Participant.ID)
	assert.Equal(t, []string{"STUDENT"}, added[0].Participant.Roles)

	query := url.Values{"page": {"1"}, "pageSize": {"1"}}
	code, body = callExperimentAPI(
		t,
		mux,
		http.MethodGet,
		"/api/experiments/"+experimentID.String()+"/participants?"+query.Encode(),
		"",
	)
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	var page paginatedExperimentParticipantsResponse
	require.NoError(t, json.Unmarshal(body, &page))
	assert.Len(t, page.Items, 1)
	assert.Equal(t, int32(2), page.TotalItems)
	assert.Equal(t, int32(2), page.TotalPages)
	assert.True(t, page.HasNextPage)

	deletePath := "/api/experiments/" + experimentID.String() + "/participants/" + participantIDs[0].String()
	code, body = callExperimentAPI(t, mux, http.MethodDelete, deletePath, "")
	require.Equal(t, http.StatusNoContent, code, "response body: %s", body)
	code, body = callExperimentAPI(t, mux, http.MethodDelete, deletePath, "")
	require.Equal(t, http.StatusNoContent, code, "repeated deletion must be idempotent: %s", body)

	code, body = callExperimentAPI(
		t,
		mux,
		http.MethodGet,
		"/api/experiments/"+experimentID.String()+"/participants",
		"",
	)
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	require.NoError(t, json.Unmarshal(body, &page))
	assert.Equal(t, int32(1), page.TotalItems)
	assert.Equal(t, participantIDs[1], page.Items[0].Participant.ID)
}

func TestAPIAddParticipantsRejectsConflictsAtomically(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	participantIDs := seedParticipantUsers(t, pool, 2)
	missingUserID := uuid.New()
	start := time.Date(2026, time.September, 11, 1, 0, 0, 0, time.UTC)
	firstExperimentID := createExperimentViaAPI(t, mux, "First assignment", start, start.Add(2*time.Hour))
	overlappingExperimentID := createExperimentViaAPI(t, mux, "Overlapping assignment", start.Add(time.Hour), start.Add(3*time.Hour))
	adjacentExperimentID := createExperimentViaAPI(t, mux, "Adjacent assignment", start.Add(2*time.Hour), start.Add(4*time.Hour))

	addParticipantsViaAPI(t, mux, firstExperimentID, []uuid.UUID{participantIDs[0]}, http.StatusOK)

	tests := []struct {
		name         string
		experimentID uuid.UUID
		userIDs      []uuid.UUID
		wantCount    int
	}{
		{
			name:         "existing assignment rolls back new user",
			experimentID: firstExperimentID,
			userIDs:      participantIDs,
			wantCount:    1,
		},
		{
			name:         "overlap rolls back every requested assignment",
			experimentID: overlappingExperimentID,
			userIDs:      participantIDs,
		},
		{
			name:         "missing user rolls back valid user",
			experimentID: adjacentExperimentID,
			userIDs:      []uuid.UUID{participantIDs[1], missingUserID},
		},
		{
			name:         "duplicate request IDs conflict",
			experimentID: adjacentExperimentID,
			userIDs:      []uuid.UUID{participantIDs[1], participantIDs[1]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addParticipantsViaAPI(t, mux, tt.experimentID, tt.userIDs, http.StatusConflict)
			assertExperimentParticipantCount(t, pool, tt.experimentID, tt.wantCount)
		})
	}

	addParticipantsViaAPI(t, mux, adjacentExperimentID, []uuid.UUID{participantIDs[0]}, http.StatusOK)
	assertExperimentParticipantCount(t, pool, adjacentExperimentID, 1)
}

func TestAPIParticipantOperationsReturnExperimentNotFound(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	missingExperimentID := uuid.New()
	participantID := uuid.New()
	requestBody, err := json.Marshal(map[string]any{"userIds": []uuid.UUID{participantID}})
	require.NoError(t, err)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/experiments/" + missingExperimentID.String() + "/participants"},
		{name: "add", method: http.MethodPost, path: "/api/experiments/" + missingExperimentID.String() + "/participants", body: string(requestBody)},
		{name: "remove", method: http.MethodDelete, path: "/api/experiments/" + missingExperimentID.String() + "/participants/" + participantID.String()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := callExperimentAPI(t, mux, tt.method, tt.path, tt.body)
			assert.Equal(t, http.StatusNotFound, code, "response body: %s", body)
		})
	}
}

func seedParticipantUsers(t *testing.T, pool *pgxpool.Pool, count int) []uuid.UUID {
	t.Helper()

	ids := make([]uuid.UUID, 0, count)
	for range count {
		id := uuid.New()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO users (id, email, name, roles)
			VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[])
		`, id, "participant-"+id.String()+"@example.test", "Participant "+id.String())
		require.NoError(t, err)
		ids = append(ids, id)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			//nolint:errcheck // best-effort cleanup for an isolated integration fixture
			pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", id)
		}
	})
	return ids
}

func createExperimentViaAPI(
	t *testing.T,
	mux *http.ServeMux,
	name string,
	start time.Time,
	end time.Time,
) uuid.UUID {
	t.Helper()

	configuration := Configuration{
		MaxAttempts:              1,
		GradingMode:              GradingModeAutomatic,
		CorrectAnswerReleaseMode: CorrectAnswerReleaseNever,
	}
	code, body := callExperimentAPI(
		t,
		mux,
		http.MethodPost,
		"/api/experiments",
		experimentRequestBody(t, name, "", start, end, configuration),
	)
	require.Equal(t, http.StatusCreated, code, "response body: %s", body)
	return decodeExperimentDetail(t, body).ID
}

func addParticipantsViaAPI(
	t *testing.T,
	mux *http.ServeMux,
	experimentID uuid.UUID,
	userIDs []uuid.UUID,
	wantCode int,
) {
	t.Helper()

	body, err := json.Marshal(map[string]any{"userIds": userIDs})
	require.NoError(t, err)
	code, response := callExperimentAPI(
		t,
		mux,
		http.MethodPost,
		"/api/experiments/"+experimentID.String()+"/participants",
		string(body),
	)
	require.Equal(t, wantCode, code, "response body: %s", response)
}

func assertExperimentParticipantCount(t *testing.T, pool *pgxpool.Pool, experimentID uuid.UUID, want int) {
	t.Helper()

	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM experiment_participants
		WHERE experiment_id = $1
	`, experimentID).Scan(&count))
	assert.Equal(t, want, count)
}
