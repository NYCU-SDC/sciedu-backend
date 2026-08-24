//go:build integration

package database_test

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const mockUserID = "00000000-0000-0000-0000-000000000001"

func TestCourseExperimentMigrationRelationship(t *testing.T) {
	databaseURL := os.Getenv("MIGRATION_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_INTEGRATION_DATABASE_URL is not set")
	}

	sourceURL := migrationSourceURL(t)
	logger := zap.NewNop()

	require.NoError(t, databaseutil.MigrationDown(sourceURL, databaseURL, logger))
	t.Cleanup(func() {
		if err := databaseutil.MigrationUp(sourceURL, databaseURL, logger); err != nil {
			t.Errorf("restore migration database to the latest version: %v", err)
		}
	})

	require.NoError(t, databaseutil.MigrationUp(sourceURL, databaseURL, logger))
	assertMigrationVersion(t, databaseURL, 15)
	assertCourseExperimentForeignKey(t, databaseURL)

	require.NoError(t, databaseutil.MigrationDown(sourceURL, databaseURL, logger))
	assertDomainTablesAbsent(t, databaseURL)

	require.NoError(t, databaseutil.MigrationUp(sourceURL, databaseURL, logger))
	assertMigrationVersion(t, databaseURL, 15)
	assertCourseForeignKeyDefinition(t, databaseURL)
}

func migrationSourceURL(t *testing.T) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "resolve migration test location")
	migrationsPath := filepath.Join(filepath.Dir(currentFile), "migrations")
	return (&url.URL{Scheme: "file", Path: migrationsPath}).String()
}

func newMigrationPool(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(t.Context()))
	return pool
}

func assertMigrationVersion(t *testing.T, databaseURL string, wantVersion int64) {
	t.Helper()

	pool := newMigrationPool(t, databaseURL)
	defer pool.Close()
	var (
		version int64
		dirty   bool
	)
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty))
	assert.Equal(t, wantVersion, version)
	assert.False(t, dirty)
}

func assertCourseExperimentForeignKey(t *testing.T, databaseURL string) {
	t.Helper()

	ctx := t.Context()
	pool := newMigrationPool(t, databaseURL)
	defer pool.Close()

	var courseID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		"INSERT INTO courses (code, title) VALUES ($1, $2) RETURNING id",
		"phase2-"+uuid.NewString(), "Phase 2 migration course",
	).Scan(&courseID))

	configuration := `{"maxAttempts":3,"allowRetry":true,"showScore":true,"showExplanations":true,"gradingMode":"LATEST","correctAnswerReleaseMode":"AFTER_SUBMISSION"}`
	var experimentID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO experiments (
			created_by, name, configuration, scheduled_start_at, scheduled_end_at
		) VALUES ($1, $2, $3::jsonb, $4, $5)
		RETURNING id`,
		mockUserID,
		"Phase 2 migration experiment",
		configuration,
		time.Now().Add(-time.Hour),
		time.Now().Add(time.Hour),
	).Scan(&experimentID))

	_, err := pool.Exec(ctx,
		"INSERT INTO experiment_courses (experiment_id, course_id) VALUES ($1, $2)",
		experimentID, uuid.New(),
	)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23503", pgErr.Code)

	_, err = pool.Exec(ctx,
		"INSERT INTO experiment_courses (experiment_id, course_id) VALUES ($1, $2)",
		experimentID, courseID,
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "DELETE FROM courses WHERE id = $1", courseID)
	require.NoError(t, err)

	var links int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM experiment_courses WHERE experiment_id = $1 AND course_id = $2",
		experimentID, courseID,
	).Scan(&links))
	assert.Zero(t, links)
}

func assertDomainTablesAbsent(t *testing.T, databaseURL string) {
	t.Helper()

	pool := newMigrationPool(t, databaseURL)
	defer pool.Close()
	for _, table := range []string{"courses", "pages", "page_blocks", "experiments", "experiment_courses"} {
		var relation *string
		require.NoError(t, pool.QueryRow(t.Context(), "SELECT to_regclass($1)", "public."+table).Scan(&relation))
		assert.Nil(t, relation, "%s should be removed by migration down", table)
	}
}

func assertCourseForeignKeyDefinition(t *testing.T, databaseURL string) {
	t.Helper()

	pool := newMigrationPool(t, databaseURL)
	defer pool.Close()
	var definition string
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'experiment_courses'::regclass
		  AND conname = 'experiment_courses_course_id_fkey'
	`).Scan(&definition))
	assert.Contains(t, definition, "FOREIGN KEY (course_id) REFERENCES courses(id) ON DELETE CASCADE")
}
