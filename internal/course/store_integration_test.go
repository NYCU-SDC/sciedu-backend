//go:build integration

package course

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newIntegrationStore(t *testing.T) (*Store, *pgxpool.Pool, string) {
	t.Helper()
	databaseURL := os.Getenv("COURSE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COURSE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx := t.Context()
	pool, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, pool.Ping(ctx))

	prefix := "course-integration-" + uuid.NewString() + "-"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM courses WHERE code LIKE $1", prefix+"%")
	})
	return NewStore(pool), pool, prefix
}

func TestStoreCourseLifecycle(t *testing.T) {
	store, _, prefix := newIntegrationStore(t)
	ctx := t.Context()
	description := "Initial description"

	created, err := store.Create(ctx, CreateParams{
		Code:        prefix + "BIO101",
		Title:       "Introduction to Biology",
		Description: &description,
	})
	require.NoError(t, err)
	assert.Equal(t, CourseStatusDRAFT, created.Status)
	assert.Equal(t, &description, created.Description)
	assert.False(t, created.CreatedAt.IsZero())
	assert.False(t, created.UpdatedAt.IsZero())

	fetched, err := store.ByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, fetched)

	updated, err := store.Update(ctx, UpdateParams{
		ID:    created.ID,
		Code:  prefix + "BIO102",
		Title: "Advanced Biology",
	})
	require.NoError(t, err)
	assert.Equal(t, "Advanced Biology", updated.Title)
	assert.Nil(t, updated.Description)
	assert.Equal(t, CourseStatusDRAFT, updated.Status)

	published, err := store.UpdateStatus(ctx, created.ID, CourseStatusPUBLISHED)
	require.NoError(t, err)
	assert.Equal(t, CourseStatusPUBLISHED, published.Status)
	assert.Equal(t, created.CreatedAt, published.CreatedAt)
}

func TestStoreListAndCountCourses(t *testing.T) {
	store, _, prefix := newIntegrationStore(t)
	ctx := t.Context()

	first, err := store.Create(ctx, CreateParams{Code: prefix + "BIO101", Title: prefix + "Biology"})
	require.NoError(t, err)
	_, err = store.Create(ctx, CreateParams{Code: prefix + "CHEM101", Title: prefix + "Chemistry"})
	require.NoError(t, err)
	_, err = store.Create(ctx, CreateParams{Code: prefix + "%LITERAL", Title: prefix + "Percent"})
	require.NoError(t, err)
	_, err = store.UpdateStatus(ctx, first.ID, CourseStatusPUBLISHED)
	require.NoError(t, err)

	t.Run("filters by status", func(t *testing.T) {
		status := CourseStatusPUBLISHED
		search := prefix
		records, err := store.List(ctx, ListParams{ListFilter: ListFilter{Status: &status, Search: &search}, Limit: 10})
		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, first.ID, records[0].ID)
	})

	t.Run("searches code and title case insensitively", func(t *testing.T) {
		search := strings.ToLower(prefix + "chem")
		records, err := store.List(ctx, ListParams{ListFilter: ListFilter{Search: &search}, Limit: 10})
		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, prefix+"Chemistry", records[0].Title)
	})

	t.Run("treats wildcard characters literally", func(t *testing.T) {
		search := prefix + "%"
		records, err := store.List(ctx, ListParams{ListFilter: ListFilter{Search: &search}, Limit: 10})
		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, prefix+"%LITERAL", records[0].Code)
	})

	t.Run("applies pagination and count", func(t *testing.T) {
		search := prefix
		total, err := store.Count(ctx, ListFilter{Search: &search})
		require.NoError(t, err)
		assert.Equal(t, int64(3), total)

		page, err := store.List(ctx, ListParams{
			ListFilter: ListFilter{Search: &search},
			Limit:      1,
			Offset:     1,
		})
		require.NoError(t, err)
		assert.Len(t, page, 1)
	})
}

func TestCourseDatabaseConstraints(t *testing.T) {
	store, pool, prefix := newIntegrationStore(t)
	ctx := t.Context()

	_, err := store.Create(ctx, CreateParams{Code: prefix + "UNIQUE", Title: "First"})
	require.NoError(t, err)

	tests := []struct {
		name        string
		code        string
		title       string
		description *string
		wantCode    string
	}{
		{name: "case insensitive duplicate code", code: strings.ToUpper(prefix + "unique"), title: "Duplicate", wantCode: "23505"},
		{name: "blank code", code: "   ", title: "Valid title", wantCode: "23514"},
		{name: "blank title", code: prefix + "BLANK-TITLE", title: "   ", wantCode: "23514"},
		{name: "code too long", code: strings.Repeat("x", 101), title: "Valid title", wantCode: "22001"},
		{name: "title too long", code: prefix + "LONG-TITLE", title: strings.Repeat("x", 201), wantCode: "22001"},
		{name: "description too long", code: prefix + "LONG-DESCRIPTION", title: "Valid title", description: stringPtr(strings.Repeat("x", 4001)), wantCode: "22001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Create(ctx, CreateParams{Code: tt.code, Title: tt.title, Description: tt.description})
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			assert.Equal(t, tt.wantCode, pgErr.Code)
		})
	}

	var status string
	err = pool.QueryRow(ctx, `SELECT status::text FROM courses WHERE code = $1`, prefix+"UNIQUE").Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "DRAFT", status)

	_, err = store.ByID(ctx, uuid.New())
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func stringPtr(value string) *string {
	return &value
}
