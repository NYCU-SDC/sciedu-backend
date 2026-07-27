//go:build integration

package user

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newIntegrationStore(t *testing.T) (*Store, *pgxpool.Pool, string) {
	t.Helper()
	databaseURL := os.Getenv("AUTH_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AUTH_INTEGRATION_DATABASE_URL is not set")
	}

	ctx := t.Context()
	pool, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, pool.Ping(ctx))

	prefix := "user-integration-" + uuid.NewString() + "-"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE email LIKE $1", prefix+"%")
	})
	return NewStore(pool), pool, prefix
}

func insertUser(t *testing.T, pool *pgxpool.Pool, email, name string, roles []string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(t.Context(),
		`INSERT INTO users (email, name, roles) VALUES ($1, $2, $3::text[]::user_role[]) RETURNING id`,
		email, name, roles).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestStoreSoftDeleteRevokesFamilies(t *testing.T) {
	store, pool, prefix := newIntegrationStore(t)
	ctx := t.Context()

	userID := insertUser(t, pool, prefix+"delete@example.com", "Delete Me", []string{"STUDENT"})

	var familyID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO refresh_token_families (user_id, expires_at) VALUES ($1, now() + interval '1 day') RETURNING id`,
		userID).Scan(&familyID))

	require.NoError(t, store.SoftDelete(ctx, userID))

	// The user is now inactive.
	_, err := store.GetActiveUser(ctx, userID)
	assert.ErrorIs(t, err, pgx.ErrNoRows)

	// The family is revoked with the expected reason, in the same operation.
	var revokedReason *string
	var revokedAt *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT revoked_reason, revoked_at::text FROM refresh_token_families WHERE id = $1`,
		familyID).Scan(&revokedReason, &revokedAt))
	require.NotNil(t, revokedReason)
	assert.Equal(t, "user_disabled", *revokedReason)
	assert.NotNil(t, revokedAt)

	// Deleting an already-disabled user is a no-op that reports not found.
	assert.ErrorIs(t, store.SoftDelete(ctx, userID), pgx.ErrNoRows)
}

func TestStoreReplaceRoles(t *testing.T) {
	store, pool, prefix := newIntegrationStore(t)
	ctx := t.Context()

	userID := insertUser(t, pool, prefix+"roles@example.com", "Role User", []string{"STUDENT"})

	updated, err := store.ReplaceRoles(ctx, userID, []string{"EXPERIMENTER", "ADMIN"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"EXPERIMENTER", "ADMIN"}, updated.Roles)

	fetched, err := store.GetActiveUser(ctx, userID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"EXPERIMENTER", "ADMIN"}, fetched.Roles)

	require.NoError(t, store.SoftDelete(ctx, userID))
	_, err = store.ReplaceRoles(ctx, userID, []string{"STUDENT"})
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestStoreListAndCount(t *testing.T) {
	store, pool, prefix := newIntegrationStore(t)
	ctx := t.Context()

	// Distinct names inserted out of order to prove ORDER BY name.
	insertUser(t, pool, prefix+"charlie@example.com", prefix+"Charlie", []string{"STUDENT"})
	insertUser(t, pool, prefix+"alice@example.com", prefix+"Alice", []string{"EXPERIMENTER"})
	insertUser(t, pool, prefix+"bob@example.com", prefix+"Bob", []string{"STUDENT"})
	disabledID := insertUser(t, pool, prefix+"dave@example.com", prefix+"Dave", []string{"STUDENT"})
	require.NoError(t, store.SoftDelete(ctx, disabledID))

	search := prefix

	t.Run("orders by name and excludes disabled", func(t *testing.T) {
		total, err := store.CountUsers(ctx, ListFilter{Search: &search})
		require.NoError(t, err)
		assert.Equal(t, int64(3), total)

		users, err := store.ListUsers(ctx, ListParams{ListFilter: ListFilter{Search: &search}, Limit: 10, Offset: 0})
		require.NoError(t, err)
		require.Len(t, users, 3)
		assert.Equal(t, prefix+"Alice", users[0].Name)
		assert.Equal(t, prefix+"Bob", users[1].Name)
		assert.Equal(t, prefix+"Charlie", users[2].Name)
	})

	t.Run("role filter", func(t *testing.T) {
		role := "EXPERIMENTER"
		users, err := store.ListUsers(ctx, ListParams{ListFilter: ListFilter{Search: &search, Role: &role}, Limit: 10, Offset: 0})
		require.NoError(t, err)
		require.Len(t, users, 1)
		assert.Equal(t, prefix+"Alice", users[0].Name)
	})

	t.Run("pagination applies limit and offset", func(t *testing.T) {
		page1, err := store.ListUsers(ctx, ListParams{ListFilter: ListFilter{Search: &search}, Limit: 2, Offset: 0})
		require.NoError(t, err)
		require.Len(t, page1, 2)
		assert.Equal(t, prefix+"Alice", page1[0].Name)
		assert.Equal(t, prefix+"Bob", page1[1].Name)

		page2, err := store.ListUsers(ctx, ListParams{ListFilter: ListFilter{Search: &search}, Limit: 2, Offset: 2})
		require.NoError(t, err)
		require.Len(t, page2, 1)
		assert.Equal(t, prefix+"Charlie", page2[0].Name)
	})

	t.Run("email substring search", func(t *testing.T) {
		emailNeedle := prefix + "alice"
		users, err := store.ListUsers(ctx, ListParams{ListFilter: ListFilter{Search: &emailNeedle}, Limit: 10, Offset: 0})
		require.NoError(t, err)
		require.Len(t, users, 1)
		assert.Equal(t, prefix+"Alice", users[0].Name)
	})
}
