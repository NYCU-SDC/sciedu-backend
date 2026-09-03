//go:build integration

package experiment

import (
	"context"
	"os"
	"testing"
	"time"

	"sciedu-backend/internal/course"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type accessScenario struct {
	participant      bool
	assigned         bool
	experimentStatus Status
	courseStatus     string
	start            time.Time
	end              time.Time
	wantAccess       bool
}

func TestCourseStoreCourseForStudentTruthTable(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	now := time.Now()

	tests := []struct {
		name     string
		scenario accessScenario
	}{
		{
			name: "current active experiment grants published assigned course",
			scenario: accessScenario{
				participant: true, assigned: true, experimentStatus: StatusActive,
				courseStatus: "PUBLISHED", start: now.Add(-time.Hour), end: now.Add(time.Hour), wantAccess: true,
			},
		},
		{
			name: "non participant is denied",
			scenario: accessScenario{
				assigned: true, experimentStatus: StatusActive,
				courseStatus: "PUBLISHED", start: now.Add(-time.Hour), end: now.Add(time.Hour),
			},
		},
		{
			name: "draft experiment is denied",
			scenario: accessScenario{
				participant: true, assigned: true, experimentStatus: StatusDraft,
				courseStatus: "PUBLISHED", start: now.Add(-time.Hour), end: now.Add(time.Hour),
			},
		},
		{
			name: "completed experiment is denied",
			scenario: accessScenario{
				participant: true, assigned: true, experimentStatus: StatusCompleted,
				courseStatus: "PUBLISHED", start: now.Add(-time.Hour), end: now.Add(time.Hour),
			},
		},
		{
			name: "future experiment is denied",
			scenario: accessScenario{
				participant: true, assigned: true, experimentStatus: StatusActive,
				courseStatus: "PUBLISHED", start: now.Add(time.Hour), end: now.Add(2 * time.Hour),
			},
		},
		{
			name: "expired experiment is denied",
			scenario: accessScenario{
				participant: true, assigned: true, experimentStatus: StatusActive,
				courseStatus: "PUBLISHED", start: now.Add(-2 * time.Hour), end: now.Add(-time.Hour),
			},
		},
		{
			name: "unassigned course is denied",
			scenario: accessScenario{
				participant: true, experimentStatus: StatusActive,
				courseStatus: "PUBLISHED", start: now.Add(-time.Hour), end: now.Add(time.Hour),
			},
		},
		{
			name: "draft course is denied",
			scenario: accessScenario{
				participant: true, assigned: true, experimentStatus: StatusActive,
				courseStatus: "DRAFT", start: now.Add(-time.Hour), end: now.Add(time.Hour),
			},
		},
		{
			name: "archived course is denied",
			scenario: accessScenario{
				participant: true, assigned: true, experimentStatus: StatusActive,
				courseStatus: "ARCHIVED", start: now.Add(-time.Hour), end: now.Add(time.Hour),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			studentID, courseID := seedAccessStudentAndCourse(t, pool, tt.scenario.courseStatus)
			seedExperimentAccess(t, pool, studentID, courseID, tt.scenario)

			decision, err := course.NewStore(pool).CourseForStudent(t.Context(), studentID, courseID)
			require.NoError(t, err)
			assert.Equal(t, courseID, decision.Course.ID)
			assert.Equal(t, tt.scenario.wantAccess, decision.Allowed)
		})
	}
}

func TestCourseStoreCourseForStudentAllowsAnyMatchingExperiment(t *testing.T) {
	pool := newExperimentIntegrationPool(t)
	studentID, courseID := seedAccessStudentAndCourse(t, pool, "PUBLISHED")
	now := time.Now()

	seedExperimentAccess(t, pool, studentID, courseID, accessScenario{
		participant: true, assigned: true, experimentStatus: StatusDraft,
		courseStatus: "PUBLISHED", start: now.Add(-time.Hour), end: now.Add(time.Hour),
	})
	seedExperimentAccess(t, pool, studentID, courseID, accessScenario{
		participant: true, assigned: true, experimentStatus: StatusActive,
		courseStatus: "PUBLISHED", start: now.Add(-time.Hour), end: now.Add(time.Hour),
	})

	decision, err := course.NewStore(pool).CourseForStudent(t.Context(), studentID, courseID)
	require.NoError(t, err)
	assert.Equal(t, courseID, decision.Course.ID)
	assert.True(t, decision.Allowed)
}

func newExperimentIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("EXPERIMENT_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("EXPERIMENT_INTEGRATION_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(t.Context()))
	t.Cleanup(pool.Close)
	return pool
}

func seedAccessStudentAndCourse(t *testing.T, pool *pgxpool.Pool, courseStatus string) (uuid.UUID, uuid.UUID) {
	t.Helper()

	ctx := t.Context()
	studentID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, name, roles)
		VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[])
	`, studentID, "phase3-"+studentID.String()+"@example.test", "Phase 3 student")
	require.NoError(t, err)

	var courseID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO courses (code, title, status)
		VALUES ($1, $2, $3::course_status)
		RETURNING id
	`, "phase3-"+uuid.NewString(), "Phase 3 course", courseStatus).Scan(&courseID))

	t.Cleanup(func() {
		//nolint:errcheck // best-effort cleanup for an isolated integration fixture
		pool.Exec(context.Background(), "DELETE FROM courses WHERE id = $1", courseID)
		//nolint:errcheck // best-effort cleanup for an isolated integration fixture
		pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", studentID)
	})
	return studentID, courseID
}

func seedExperimentAccess(
	t *testing.T,
	pool *pgxpool.Pool,
	studentID uuid.UUID,
	courseID uuid.UUID,
	scenario accessScenario,
) {
	t.Helper()

	configuration := `{"maxAttempts":3,"allowRetry":true,"showScore":true,"showExplanations":true,"gradingMode":"AUTOMATIC","correctAnswerReleaseMode":"NEVER"}`
	var experimentID uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), `
		INSERT INTO experiments (
			created_by, name, configuration, status, scheduled_start_at, scheduled_end_at
		) VALUES ($1, $2, $3::jsonb, $4::experiment_status, $5, $6)
		RETURNING id
	`, studentID, "Phase 3 experiment", configuration, scenario.experimentStatus, scenario.start, scenario.end).Scan(&experimentID))

	if scenario.participant {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO experiment_participants (experiment_id, user_id)
			VALUES ($1, $2)
		`, experimentID, studentID)
		require.NoError(t, err)
	}
	if scenario.assigned {
		_, err := pool.Exec(t.Context(), `
			INSERT INTO experiment_courses (experiment_id, course_id)
			VALUES ($1, $2)
		`, experimentID, courseID)
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		//nolint:errcheck // cascades remove participant and course links
		pool.Exec(context.Background(), "DELETE FROM experiments WHERE id = $1", experimentID)
	})
}
