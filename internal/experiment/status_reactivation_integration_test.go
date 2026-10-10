//go:build integration

package experiment

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIExperimentReactivationChecksParticipantSchedules(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	base := time.Date(2026, time.October, 10, 9, 0, 0, 0, time.UTC)

	t.Run("reviewer scenario rejects overlapping reactivation", func(t *testing.T) {
		participantID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
		firstID := seedExperimentForUpdate(t, pool, actorID, StatusActive, "Reactivation first", base, base.Add(2*time.Hour), configuration)
		_, err := pool.Exec(t.Context(), `INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)`, firstID, participantID)
		require.NoError(t, err)

		code, body := callExperimentAPI(t, mux, http.MethodPut, "/api/experiments/"+firstID.String()+"/status", `{"status":"ARCHIVED"}`)
		require.Equal(t, http.StatusOK, code, "response body: %s", body)

		secondID := seedExperimentForUpdate(t, pool, actorID, StatusActive, "Reactivation second", base.Add(time.Hour), base.Add(3*time.Hour), configuration)
		code, body = callExperimentAPI(t, mux, http.MethodPost, "/api/experiments/"+secondID.String()+"/participants", participantBatchBody(t, participantID))
		require.Equal(t, http.StatusOK, code, "response body: %s", body)

		code, body = callExperimentAPI(t, mux, http.MethodPut, "/api/experiments/"+firstID.String()+"/status", `{"status":"ACTIVE"}`)
		require.Equal(t, http.StatusConflict, code, "response body: %s", body)
		assert.Equal(t, StatusArchived, experimentStatusByID(t, pool, firstID))
		assert.Equal(t, StatusActive, experimentStatusByID(t, pool, secondID))
	})

	tests := []struct {
		name             string
		withParticipant  bool
		otherStartOffset time.Duration
		otherEndOffset   time.Duration
	}{
		{name: "no participants succeeds"},
		{name: "boundary touch succeeds", withParticipant: true, otherStartOffset: time.Hour, otherEndOffset: 2 * time.Hour},
		{name: "disjoint schedule succeeds", withParticipant: true, otherStartOffset: 2 * time.Hour, otherEndOffset: 3 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			participantID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
			targetID := seedExperimentForUpdate(t, pool, actorID, StatusArchived, "Reactivate "+tt.name, base, base.Add(time.Hour), configuration)
			if tt.withParticipant {
				otherID := seedExperimentForUpdate(t, pool, actorID, StatusActive, "Other "+tt.name, base.Add(tt.otherStartOffset), base.Add(tt.otherEndOffset), configuration)
				_, err := pool.Exec(t.Context(), `
					INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $3), ($2, $3)
				`, targetID, otherID, participantID)
				require.NoError(t, err)
			}

			code, body := callExperimentAPI(t, mux, http.MethodPut, "/api/experiments/"+targetID.String()+"/status", `{"status":"ACTIVE"}`)
			require.Equal(t, http.StatusOK, code, "response body: %s", body)
			assert.Equal(t, StatusActive, experimentStatusByID(t, pool, targetID))
		})
	}
}

func TestAPIConcurrentOverlappingReactivations(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	actorID := seedExperimentAPIActor(t, pool)
	mux := newExperimentAPI(t, pool, actorID)
	participantID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	start := time.Date(2026, time.October, 11, 9, 0, 0, 0, time.UTC)
	firstID := seedExperimentForUpdate(t, pool, actorID, StatusArchived, "Concurrent reactivation first", start, start.Add(2*time.Hour), configuration)
	secondID := seedExperimentForUpdate(t, pool, actorID, StatusArchived, "Concurrent reactivation second", start.Add(time.Hour), start.Add(3*time.Hour), configuration)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $3), ($2, $3)
	`, firstID, secondID, participantID)
	require.NoError(t, err)

	startRequests := make(chan struct{})
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for _, experimentID := range []uuid.UUID{firstID, secondID} {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			<-startRequests
			request := httptest.NewRequest(http.MethodPut, "/api/experiments/"+id.String()+"/status", strings.NewReader(`{"status":"ACTIVE"}`))
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

	var activeCount int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*) FROM experiments WHERE id = ANY($1::uuid[]) AND status = 'ACTIVE'
	`, []uuid.UUID{firstID, secondID}).Scan(&activeCount))
	assert.Equal(t, 1, activeCount)
}

func experimentStatusByID(t *testing.T, pool *pgxpool.Pool, experimentID uuid.UUID) Status {
	t.Helper()
	var status Status
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT status FROM experiments WHERE id = $1", experimentID).Scan(&status))
	return status
}
