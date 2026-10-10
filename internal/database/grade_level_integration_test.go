//go:build integration

package database_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestGradeLevelMigration(t *testing.T) {
	databaseURL := os.Getenv("GRADE_LEVEL_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GRADE_LEVEL_INTEGRATION_DATABASE_URL is not set")
	}
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, databaseURL)
	require.NoError(t, err)
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	// pg_temp shadows public.users: all migration writes stay in this session.
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE users (id INTEGER PRIMARY KEY);
        INSERT INTO users (id) VALUES (1);
        SET LOCAL search_path TO pg_temp`)
	require.NoError(t, err)
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	readMigration := func(suffix string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "migrations", "22_user_grade_level."+suffix+".sql"))
		require.NoError(t, err)
		return string(data)
	}
	_, err = tx.Exec(ctx, readMigration("up"))
	require.NoError(t, err)
	var level pgtype.Int4
	require.NoError(t, tx.QueryRow(ctx, "SELECT grade_level FROM users WHERE id = 1").Scan(&level))
	require.False(t, level.Valid, "existing users need no backfill")

	for grade := 7; grade <= 12; grade++ {
		_, err = tx.Exec(ctx, "UPDATE users SET grade_level = $1 WHERE id = 1", grade)
		require.NoError(t, err)
		require.NoError(t, tx.QueryRow(ctx, "SELECT grade_level FROM users WHERE id = 1").Scan(&level))
		require.Equal(t, int32(grade), level.Int32)
	}
	for _, grade := range []int{6, 13} {
		_, err = tx.Exec(ctx, "SAVEPOINT invalid_grade")
		require.NoError(t, err)
		_, err = tx.Exec(ctx, "UPDATE users SET grade_level = $1 WHERE id = 1", grade)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "23514", pgErr.Code)
		require.Equal(t, "users_grade_level_check", pgErr.ConstraintName)
		_, err = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT invalid_grade")
		require.NoError(t, err)
	}
	_, err = tx.Exec(ctx, "UPDATE users SET grade_level = NULL WHERE id = 1")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, readMigration("down"))
	require.NoError(t, err)
	var id int
	require.NoError(t, tx.QueryRow(ctx, "SELECT * FROM users WHERE id = 1").Scan(&id))
	require.Equal(t, 1, id, "rollback preserves users")
}
