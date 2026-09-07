//go:build integration

package experiment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"sciedu-backend/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedParticipantCandidate(t *testing.T, pool *pgxpool.Pool, roles []string, disabledAt *time.Time) uuid.UUID {
	t.Helper()

	userID := uuid.New()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, name, avatar_url, roles, disabled_at)
		VALUES (
			$1, $2, $3, $4,
			ARRAY(SELECT role::user_role FROM unnest($5::text[]) AS role),
			$6
		)
	`, userID, "participant-"+userID.String()+"@example.test", "Participant "+userID.String(), "https://example.test/avatar.png", roles, disabledAt)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup for an isolated integration fixture
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})
	return userID
}

func participantBatchBody(t *testing.T, userIDs ...uuid.UUID) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"userIds": userIDs})
	require.NoError(t, err)
	return string(body)
}

func countParticipantAssignments(t *testing.T, pool *pgxpool.Pool, experimentID uuid.UUID) int {
	t.Helper()
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM experiment_participants WHERE experiment_id = $1
	`, experimentID).Scan(&count))
	return count
}

func TestAPIParticipantAssignmentLifecycle(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	start := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	configuration := Configuration{
		MaxAttempts:              1,
		GradingMode:              GradingModeAutomatic,
		CorrectAnswerReleaseMode: CorrectAnswerReleaseNever,
	}

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
			experimentID := seedExperimentForUpdate(t, pool, actorID, tt.status, "Participant lifecycle "+string(tt.status), start, start.Add(time.Hour), configuration)
			participantID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)

			code, body := callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+experimentID.String()+"/participants", participantBatchBody(t, participantID))
			require.Equal(t, tt.wantAddCode, code, "response body: %s", body)
			if tt.wantAddCode != http.StatusOK {
				assert.Zero(t, countParticipantAssignments(t, pool, experimentID))
				code, body = callExperimentAPI(t, mux, http.MethodDelete, "/api/experiments/"+experimentID.String()+"/participants/"+participantID.String(), "")
				require.Equal(t, tt.wantDeleteCode, code, "response body: %s", body)
				return
			}

			var assignments []experimentParticipantAssignmentResponse
			require.NoError(t, json.Unmarshal(body, &assignments))
			require.Len(t, assignments, 1)
			assert.Equal(t, participantID, assignments[0].Participant.ID)
			assert.Equal(t, []string{"STUDENT"}, assignments[0].Participant.Roles)
			assert.Equal(t, 1, countParticipantAssignments(t, pool, experimentID))

			code, body = callExperimentAPI(t, mux, http.MethodDelete, "/api/experiments/"+experimentID.String()+"/participants/"+participantID.String(), "")
			require.Equal(t, tt.wantDeleteCode, code, "response body: %s", body)
			if tt.wantDeleteCode == http.StatusNoContent {
				assert.Zero(t, countParticipantAssignments(t, pool, experimentID))
			} else {
				assert.Equal(t, 1, countParticipantAssignments(t, pool, experimentID))
			}
		})
	}

	draftID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Idempotent removal", start.Add(2*time.Hour), start.Add(3*time.Hour), configuration)
	missingParticipantID := uuid.New()
	code, body := callExperimentAPI(t, mux, http.MethodDelete, "/api/experiments/"+draftID.String()+"/participants/"+missingParticipantID.String(), "")
	require.Equal(t, http.StatusNoContent, code, "response body: %s", body)

	missingExperimentID := uuid.New()
	code, body = callExperimentAPI(t, mux, http.MethodDelete, "/api/experiments/"+missingExperimentID.String()+"/participants/"+missingParticipantID.String(), "")
	assert.Equal(t, http.StatusNotFound, code, "response body: %s", body)

	participantID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	code, body = callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+missingExperimentID.String()+"/participants", participantBatchBody(t, participantID))
	assert.Equal(t, http.StatusNotFound, code, "response body: %s", body)
}

func TestAPIParticipantEligibilityAndAtomicBatch(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	start := time.Date(2026, time.October, 2, 9, 0, 0, 0, time.UTC)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	disabledAt := time.Now().UTC()

	tests := []struct {
		name           string
		candidateRoles []string
		disabledAt     *time.Time
		missing        bool
		multiRole      bool
		wantCode       int
	}{
		{name: "active student", candidateRoles: []string{"STUDENT"}, wantCode: http.StatusOK},
		{name: "multi role student", candidateRoles: []string{"STUDENT", "EXPERIMENTER"}, multiRole: true, wantCode: http.StatusOK},
		{name: "disabled student", candidateRoles: []string{"STUDENT"}, disabledAt: &disabledAt, wantCode: http.StatusConflict},
		{name: "non student", candidateRoles: []string{"EXPERIMENTER"}, wantCode: http.StatusConflict},
		{name: "missing user", missing: true, wantCode: http.StatusConflict},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			experimentID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Eligibility "+tt.name, start.Add(time.Duration(index)*2*time.Hour), start.Add(time.Duration(index)*2*time.Hour+time.Hour), configuration)
			participantID := uuid.New()
			if !tt.missing {
				participantID = seedParticipantCandidate(t, pool, tt.candidateRoles, tt.disabledAt)
			}
			code, body := callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+experimentID.String()+"/participants", participantBatchBody(t, participantID))
			require.Equal(t, tt.wantCode, code, "response body: %s", body)
			if tt.wantCode == http.StatusOK {
				assert.Equal(t, 1, countParticipantAssignments(t, pool, experimentID))
			} else {
				assert.Zero(t, countParticipantAssignments(t, pool, experimentID))
			}
		})
	}

	validID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	invalidID := seedParticipantCandidate(t, pool, []string{"EXPERIMENTER"}, nil)
	batchID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Atomic batch", start.Add(12*time.Hour), start.Add(13*time.Hour), configuration)
	code, body := callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+batchID.String()+"/participants", participantBatchBody(t, validID, invalidID))
	require.Equal(t, http.StatusConflict, code, "response body: %s", body)
	assert.Zero(t, countParticipantAssignments(t, pool, batchID))

	code, body = callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+batchID.String()+"/participants", participantBatchBody(t, validID, validID))
	require.Equal(t, http.StatusConflict, code, "response body: %s", body)
	assert.Zero(t, countParticipantAssignments(t, pool, batchID))

	_, err := pool.Exec(t.Context(), `
		INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)
	`, batchID, validID)
	require.NoError(t, err)
	newValidID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	code, body = callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+batchID.String()+"/participants", participantBatchBody(t, validID, newValidID))
	require.Equal(t, http.StatusConflict, code, "response body: %s", body)
	assert.Equal(t, 1, countParticipantAssignments(t, pool, batchID))
}

func TestAPIParticipantOverlapRules(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	base := time.Date(2026, time.October, 3, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		otherStatus Status
		targetStart time.Time
		targetEnd   time.Time
		otherStart  time.Time
		otherEnd    time.Time
		wantCode    int
	}{
		{name: "partial overlap", otherStatus: StatusScheduled, targetStart: base, targetEnd: base.Add(2 * time.Hour), otherStart: base.Add(time.Hour), otherEnd: base.Add(3 * time.Hour), wantCode: http.StatusConflict},
		{name: "containment", otherStatus: StatusCompleted, targetStart: base.Add(time.Hour), targetEnd: base.Add(2 * time.Hour), otherStart: base, otherEnd: base.Add(3 * time.Hour), wantCode: http.StatusConflict},
		{name: "boundary touch", otherStatus: StatusActive, targetStart: base, targetEnd: base.Add(time.Hour), otherStart: base.Add(time.Hour), otherEnd: base.Add(2 * time.Hour), wantCode: http.StatusOK},
		{name: "disjoint", otherStatus: StatusDraft, targetStart: base, targetEnd: base.Add(time.Hour), otherStart: base.Add(2 * time.Hour), otherEnd: base.Add(3 * time.Hour), wantCode: http.StatusOK},
		{name: "archived does not reserve time", otherStatus: StatusArchived, targetStart: base, targetEnd: base.Add(2 * time.Hour), otherStart: base.Add(time.Hour), otherEnd: base.Add(3 * time.Hour), wantCode: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			participantID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
			targetID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Target "+tt.name, tt.targetStart, tt.targetEnd, configuration)
			otherID := seedExperimentForUpdate(t, pool, actorID, tt.otherStatus, "Other "+tt.name, tt.otherStart, tt.otherEnd, configuration)
			_, err := pool.Exec(t.Context(), `
				INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)
			`, otherID, participantID)
			require.NoError(t, err)

			code, body := callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+targetID.String()+"/participants", participantBatchBody(t, participantID))
			require.Equal(t, tt.wantCode, code, "response body: %s", body)
			if tt.wantCode == http.StatusOK {
				assert.Equal(t, 1, countParticipantAssignments(t, pool, targetID))
			} else {
				assert.Zero(t, countParticipantAssignments(t, pool, targetID))
			}
		})
	}
}

func TestAPIParticipantListPaginationAndCounts(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	start := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	experimentID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Participant list", start, start.Add(time.Hour), configuration)
	firstID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	secondID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	firstAssignedAt := start.Add(-2 * time.Hour)
	secondAssignedAt := start.Add(-time.Hour)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO experiment_participants (experiment_id, user_id, assigned_at)
		VALUES ($1, $2, $4), ($1, $3, $5)
	`, experimentID, firstID, secondID, firstAssignedAt, secondAssignedAt)
	require.NoError(t, err)

	query := url.Values{"page": {"1"}, "pageSize": {"1"}}
	code, body := callExperimentAPI(t, mux, http.MethodGet, "/api/experiments/"+experimentID.String()+"/participants?"+query.Encode(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	var page paginatedExperimentParticipantsResponse
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, secondID, page.Items[0].Participant.ID)
	assert.Equal(t, int32(2), page.TotalItems)
	assert.Equal(t, int32(2), page.TotalPages)
	assert.True(t, page.HasNextPage)

	code, body = callExperimentAPI(t, mux, http.MethodGet, "/api/experiments/"+experimentID.String()+"/participants?page=2&pageSize=1", "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Items, 1)
	assert.Equal(t, firstID, page.Items[0].Participant.ID)

	code, body = callExperimentAPI(t, mux, http.MethodGet, "/api/experiments/"+uuid.NewString()+"/participants", "")
	assert.Equal(t, http.StatusNotFound, code, "response body: %s", body)

	code, body = callExperimentAPI(t, mux, http.MethodGet, "/api/experiments/"+experimentID.String(), "")
	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	detail := decodeExperimentDetail(t, body)
	assert.Equal(t, int32(2), detail.ParticipantCount)
}

func TestAPIConcurrentParticipantAddsToOverlappingExperiments(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	participantID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	start := time.Date(2026, time.October, 5, 9, 0, 0, 0, time.UTC)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	firstID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Concurrent first", start, start.Add(2*time.Hour), configuration)
	secondID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Concurrent second", start.Add(time.Hour), start.Add(3*time.Hour), configuration)
	requestBody := participantBatchBody(t, participantID)

	startRequests := make(chan struct{})
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for _, experimentID := range []uuid.UUID{firstID, secondID} {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			<-startRequests
			request := httptest.NewRequest(http.MethodPost, "/api/experiments/"+id.String()+"/participants", strings.NewReader(requestBody))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			results <- recorder.Code
		}(experimentID)
	}
	close(startRequests)
	wg.Wait()
	close(results)

	codes := make([]int, 0, 2)
	for code := range results {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	assert.Equal(t, []int{http.StatusOK, http.StatusConflict}, codes)

	var assignmentCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM experiment_participants
		WHERE user_id = $1 AND experiment_id = ANY($2::uuid[])
	`, participantID, []uuid.UUID{firstID, secondID}).Scan(&assignmentCount))
	assert.Equal(t, 1, assignmentCount)
}

func TestAPIParticipantAddObservesConcurrentLockedStatus(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	participantID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	start := time.Date(2026, time.October, 6, 9, 0, 0, 0, time.UTC)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	experimentID := seedExperimentForUpdate(t, pool, actorID, StatusDraft, "Status race", start, start.Add(time.Hour), configuration)

	blocker, err := pool.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.Background()) })
	_, err = blocker.Exec(t.Context(), "SELECT id FROM experiments WHERE id = $1 FOR UPDATE", experimentID)
	require.NoError(t, err)

	type response struct {
		code int
		body string
	}
	statusResult := make(chan response, 1)
	go func() {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/experiments/"+experimentID.String()+"/status", strings.NewReader(`{"status":"COMPLETED"}`))
		request.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(recorder, request)
		statusResult <- response{code: recorder.Code, body: recorder.Body.String()}
	}()
	require.Eventually(t, func() bool { return countLockWaiters(t, pool) >= 1 }, 3*time.Second, 20*time.Millisecond)

	addResult := make(chan response, 1)
	requestBody := participantBatchBody(t, participantID)
	go func() {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/experiments/"+experimentID.String()+"/participants", strings.NewReader(requestBody))
		request.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(recorder, request)
		addResult <- response{code: recorder.Code, body: recorder.Body.String()}
	}()
	require.Eventually(t, func() bool { return countLockWaiters(t, pool) >= 2 }, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, blocker.Commit(t.Context()))

	statusResponse := <-statusResult
	addResponse := <-addResult
	require.Equal(t, http.StatusOK, statusResponse.code, "response body: %s", statusResponse.body)
	require.Equal(t, http.StatusConflict, addResponse.code, "response body: %s", addResponse.body)
	assert.Zero(t, countParticipantAssignments(t, pool, experimentID))

	var status Status
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM experiments WHERE id = $1", experimentID).Scan(&status))
	assert.Equal(t, StatusCompleted, status)
}

func countLockWaiters(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM pg_stat_activity
		WHERE datname = current_database()
		  AND wait_event_type = 'Lock'
	`).Scan(&count))
	return count
}

func TestParticipantRoutesRejectStudent(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	studentID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	mux := newExperimentAPIWithRoles(t, pool, studentID, []auth.Role{auth.STUDENT})
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/experiments/"+uuid.NewString()+"/participants", nil))
	assert.Equal(t, http.StatusForbidden, recorder.Code)
}
