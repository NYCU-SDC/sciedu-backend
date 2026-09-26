//go:build integration

package experiment

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"sciedu-backend/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCurrentExperimentAPI(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	studentID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
	creatorID := seedExperimentAPIActor(t, pool)
	configuration := Configuration{MaxAttempts: 1, GradingMode: GradingModeAutomatic, CorrectAnswerReleaseMode: CorrectAnswerReleaseNever}
	now := time.Now().UTC()
	experimentID := seedExperimentForUpdate(t, pool, creatorID, StatusActive, "Current experiment", now.Add(-time.Hour), now.Add(time.Hour), configuration)
	publishedID := seedAssignmentCourse(t, pool, CourseStatusPUBLISHED)
	draftID := seedAssignmentCourse(t, pool, CourseStatusDRAFT)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)
	`, experimentID, studentID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		INSERT INTO experiment_courses (experiment_id, course_id) VALUES ($1, $2), ($1, $3)
	`, experimentID, publishedID, draftID)
	require.NoError(t, err)
	mux := newExperimentAPIWithRoles(t, pool, studentID, []auth.Role{auth.STUDENT})

	code, body := callExperimentAPI(t, mux, http.MethodGet, "/api/experiments/current", "")

	require.Equal(t, http.StatusOK, code, "response body: %s", body)
	var response currentExperimentResponse
	require.NoError(t, json.Unmarshal(body, &response))
	assert.Equal(t, experimentID, response.Experiment.ID)
	require.Len(t, response.Courses, 1)
	assert.Equal(t, publishedID, response.Courses[0].ID)
}

func TestCurrentExperimentScheduleBoundaries(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	tests := []struct {
		name      string
		status    Status
		startSQL  string
		endSQL    string
		wantFound bool
	}{
		{name: "start boundary is inclusive", status: StatusActive, startSQL: "CURRENT_TIMESTAMP", endSQL: "CURRENT_TIMESTAMP + interval '1 hour'", wantFound: true},
		{name: "end boundary is exclusive", status: StatusActive, startSQL: "CURRENT_TIMESTAMP - interval '1 hour'", endSQL: "CURRENT_TIMESTAMP"},
		{name: "future active experiment excluded", status: StatusActive, startSQL: "CURRENT_TIMESTAMP + interval '1 hour'", endSQL: "CURRENT_TIMESTAMP + interval '2 hours'"},
		{name: "non active experiment excluded", status: StatusScheduled, startSQL: "CURRENT_TIMESTAMP - interval '1 hour'", endSQL: "CURRENT_TIMESTAMP + interval '1 hour'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			studentID := seedParticipantCandidate(t, pool, []string{"STUDENT"}, nil)
			experimentID := seedCurrentBoundaryExperiment(t, pool, studentID, tt.status, tt.startSQL, tt.endSQL)

			records, err := NewStore(pool).ListCurrentForStudent(t.Context(), studentID)

			require.NoError(t, err)
			if tt.wantFound {
				require.Len(t, records, 1)
				assert.Equal(t, experimentID, records[0].ID)
			} else {
				assert.Empty(t, records)
			}
		})
	}
}

func seedCurrentBoundaryExperiment(t *testing.T, pool *pgxpool.Pool, studentID uuid.UUID, status Status, startSQL, endSQL string) uuid.UUID {
	t.Helper()
	configuration := `{"maxAttempts":1,"allowRetry":false,"showScore":false,"showExplanations":false,"gradingMode":"AUTOMATIC","correctAnswerReleaseMode":"NEVER"}`
	query := `
		INSERT INTO experiments (created_by, name, configuration, status, scheduled_start_at, scheduled_end_at)
		VALUES ($1, $2, $3::jsonb, $4::experiment_status, ` + startSQL + `, ` + endSQL + `)
		RETURNING id`
	var experimentID uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), query, studentID, "Current boundary "+uuid.NewString(), configuration, status).Scan(&experimentID))
	_, err := pool.Exec(t.Context(), `INSERT INTO experiment_participants (experiment_id, user_id) VALUES ($1, $2)`, experimentID, studentID)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup for isolated integration fixtures
		pool.Exec(context.Background(), "DELETE FROM experiments WHERE id = $1", experimentID)
	})
	return experimentID
}
