//go:build integration

package progress

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type progressFixture struct {
	studentID uuid.UUID
	otherID   uuid.UUID
	courseID  uuid.UUID
	pageA     uuid.UUID
	pageB     uuid.UUID
}

func newProgressIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("PAGE_PROGRESS_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PAGE_PROGRESS_INTEGRATION_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("create integration pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(t.Context()); err != nil {
		t.Fatalf("reach integration database: %v", err)
	}
	return pool
}

func seedProgressFixture(t *testing.T, pool *pgxpool.Pool) progressFixture {
	t.Helper()

	fixture := progressFixture{
		studentID: uuid.New(),
		otherID:   uuid.New(),
	}
	suffix := uuid.NewString()
	ctx := t.Context()

	for i, studentID := range []uuid.UUID{fixture.studentID, fixture.otherID} {
		_, err := pool.Exec(ctx,
			"INSERT INTO users (id, email, name, roles) VALUES ($1, $2, $3, ARRAY['STUDENT']::user_role[])",
			studentID, fmt.Sprintf("progress-%d-%s@example.invalid", i, suffix), "Progress Student")
		require.NoError(t, err, "seed student")
	}

	require.NoError(t, pool.QueryRow(ctx,
		"INSERT INTO courses (code, title) VALUES ($1, $2) RETURNING id",
		"progress-"+suffix, "Progress Course").Scan(&fixture.courseID), "seed course")

	require.NoError(t, pool.QueryRow(ctx,
		"INSERT INTO pages (course_id, title, display_order) VALUES ($1, $2, 0) RETURNING id",
		fixture.courseID, "Progress Page A").Scan(&fixture.pageA), "seed page A")
	require.NoError(t, pool.QueryRow(ctx,
		"INSERT INTO pages (course_id, title, display_order) VALUES ($1, $2, 1) RETURNING id",
		fixture.courseID, "Progress Page B").Scan(&fixture.pageB), "seed page B")

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		//nolint:errcheck // best-effort cleanup
		pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = ANY($1::uuid[])",
			[]uuid.UUID{fixture.studentID, fixture.otherID})
		//nolint:errcheck
		pool.Exec(cleanupCtx, "DELETE FROM pages WHERE id = ANY($1::uuid[])",
			[]uuid.UUID{fixture.pageA, fixture.pageB})
		//nolint:errcheck
		pool.Exec(cleanupCtx, "DELETE FROM courses WHERE id = $1", fixture.courseID)
	})

	return fixture
}

func TestStoreUpsertReachIsIdempotent(t *testing.T) {
	pool := newProgressIntegrationPool(t)
	fixture := seedProgressFixture(t, pool)
	store := NewStore(pool)
	ctx := t.Context()
	first := time.Now().UTC().Truncate(time.Microsecond)

	a, err := store.UpsertReach(ctx, fixture.studentID, fixture.pageA, first)
	require.NoError(t, err)
	assert.Equal(t, fixture.courseID, a.CourseID)
	assert.Equal(t, first, a.ReachedAt)
	assert.Nil(t, a.CompletedAt)

	b, err := store.UpsertReach(ctx, fixture.studentID, fixture.pageA, first.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, first, b.ReachedAt, "second reach must preserve original reached_at")
}

func TestStoreUpsertCompleteStampsReachWhenMissing(t *testing.T) {
	pool := newProgressIntegrationPool(t)
	fixture := seedProgressFixture(t, pool)
	store := NewStore(pool)
	ctx := t.Context()
	completed := time.Now().UTC().Truncate(time.Microsecond)

	row, err := store.UpsertComplete(ctx, fixture.studentID, fixture.pageA, completed)
	require.NoError(t, err)
	assert.Equal(t, completed, row.ReachedAt)
	require.NotNil(t, row.CompletedAt)
	assert.Equal(t, completed, *row.CompletedAt)
}

func TestStoreUpsertCompleteKeepsExistingReach(t *testing.T) {
	pool := newProgressIntegrationPool(t)
	fixture := seedProgressFixture(t, pool)
	store := NewStore(pool)
	ctx := t.Context()
	reached := time.Now().UTC().Truncate(time.Microsecond)
	completed := reached.Add(time.Hour)

	_, err := store.UpsertReach(ctx, fixture.studentID, fixture.pageA, reached)
	require.NoError(t, err)

	row, err := store.UpsertComplete(ctx, fixture.studentID, fixture.pageA, completed)
	require.NoError(t, err)
	assert.Equal(t, reached, row.ReachedAt, "existing reached_at must not be overwritten")
	require.NotNil(t, row.CompletedAt)
	assert.Equal(t, completed, *row.CompletedAt)
}

func TestStoreUpsertCompleteIsIrreversible(t *testing.T) {
	pool := newProgressIntegrationPool(t)
	fixture := seedProgressFixture(t, pool)
	store := NewStore(pool)
	ctx := t.Context()
	first := time.Now().UTC().Truncate(time.Microsecond)

	_, err := store.UpsertComplete(ctx, fixture.studentID, fixture.pageA, first)
	require.NoError(t, err)

	row, err := store.UpsertComplete(ctx, fixture.studentID, fixture.pageA, first.Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, row.CompletedAt)
	assert.Equal(t, first, *row.CompletedAt, "completed_at must stay at the first completion")
}

func TestStoreUpsertReachForMissingPageReturnsNotFound(t *testing.T) {
	pool := newProgressIntegrationPool(t)
	fixture := seedProgressFixture(t, pool)
	store := NewStore(pool)

	_, err := store.UpsertReach(t.Context(), fixture.studentID, uuid.New(), time.Now().UTC())
	assert.ErrorIs(t, err, ErrPageNotFound)
}

func TestStorePageCascadeDeletesProgress(t *testing.T) {
	pool := newProgressIntegrationPool(t)
	fixture := seedProgressFixture(t, pool)
	store := NewStore(pool)
	ctx := t.Context()

	_, err := store.UpsertReach(ctx, fixture.studentID, fixture.pageA, time.Now().UTC())
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "DELETE FROM pages WHERE id = $1", fixture.pageA)
	require.NoError(t, err)

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM page_progress WHERE student_id = $1 AND page_id = $2",
		fixture.studentID, fixture.pageA).Scan(&count))
	assert.Equal(t, 0, count, "page delete must cascade to page_progress")
}

func TestStoreProgressIsScopedToStudent(t *testing.T) {
	pool := newProgressIntegrationPool(t)
	fixture := seedProgressFixture(t, pool)
	store := NewStore(pool)
	ctx := t.Context()
	t0 := time.Now().UTC().Truncate(time.Microsecond)

	_, err := store.UpsertReach(ctx, fixture.studentID, fixture.pageA, t0)
	require.NoError(t, err)
	_, err = store.UpsertComplete(ctx, fixture.otherID, fixture.pageA, t0)
	require.NoError(t, err)

	rows, err := store.ProgressByStudentCourse(ctx, fixture.studentID, fixture.courseID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, fixture.pageA, rows[0].PageID)
	assert.Nil(t, rows[0].CompletedAt)

	other, err := store.ProgressByStudentCourse(ctx, fixture.otherID, fixture.courseID)
	require.NoError(t, err)
	require.Len(t, other, 1)
	require.NotNil(t, other[0].CompletedAt)
}

func TestStoreCompletedBeforeReachRejectedByCheck(t *testing.T) {
	pool := newProgressIntegrationPool(t)
	fixture := seedProgressFixture(t, pool)
	ctx := t.Context()
	reached := time.Now().UTC().Truncate(time.Microsecond)

	_, err := pool.Exec(ctx, `
		INSERT INTO page_progress (student_id, page_id, course_id, reached_at, completed_at)
		VALUES ($1, $2, $3, $4, $5)`,
		fixture.studentID, fixture.pageA, fixture.courseID, reached, reached.Add(-time.Second))
	require.Error(t, err, "CHECK must reject completed_at before reached_at")
}

func TestStorePagesByCourseOrdersByDisplayOrder(t *testing.T) {
	pool := newProgressIntegrationPool(t)
	fixture := seedProgressFixture(t, pool)
	store := NewStore(pool)

	pages, err := store.PagesByCourse(t.Context(), fixture.courseID)
	require.NoError(t, err)
	require.Len(t, pages, 2)
	assert.Equal(t, fixture.pageA, pages[0].ID)
	assert.Equal(t, fixture.pageB, pages[1].ID)
}
