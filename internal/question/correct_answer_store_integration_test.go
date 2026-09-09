//go:build integration

package question

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newQuestionIntegrationStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	databaseURL := os.Getenv("QUESTION_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("QUESTION_INTEGRATION_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, pool.Ping(t.Context()))
	return NewStore(pool), pool
}

func TestCorrectAnswerStoreVersioningAndOptionCascade(t *testing.T) {
	store, pool := newQuestionIntegrationStore(t)
	ctx := t.Context()

	var questionID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO questions (content, type) VALUES ('choice', 'CHOICE') RETURNING id`,
	).Scan(&questionID))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM questions WHERE id = $1`, questionID)
	})

	var firstOptionID, secondOptionID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO options (question_id, content, label) VALUES ($1, 'first', 'A') RETURNING id`,
		questionID,
	).Scan(&firstOptionID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO options (question_id, content, label) VALUES ($1, 'second', 'B') RETURNING id`,
		questionID,
	).Scan(&secondOptionID))

	created, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{
		QuestionID:       questionID,
		Type:             "CHOICE",
		SelectedOptionID: pgtype.UUID{Bytes: firstOptionID, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), created.Version)

	idempotent, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{
		QuestionID:       questionID,
		Type:             "CHOICE",
		SelectedOptionID: pgtype.UUID{Bytes: firstOptionID, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), idempotent.Version)
	assert.Equal(t, created.UpdatedAt, idempotent.UpdatedAt)

	changed, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{
		QuestionID:       questionID,
		Type:             "CHOICE",
		SelectedOptionID: pgtype.UUID{Bytes: secondOptionID, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), changed.Version)

	_, err = pool.Exec(ctx, `DELETE FROM options WHERE id = $1`, secondOptionID)
	require.NoError(t, err)
	_, err = store.GetCorrectAnswer(ctx, questionID)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestCorrectAnswerStoreTextRepresentation(t *testing.T) {
	store, pool := newQuestionIntegrationStore(t)
	ctx := t.Context()

	var questionID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO questions (content, type) VALUES ('text', 'TEXT') RETURNING id`,
	).Scan(&questionID))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM questions WHERE id = $1`, questionID)
	})

	reference := "reference"
	created, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{
		QuestionID:      questionID,
		Type:            "TEXT",
		ReferenceAnswer: pgtype.Text{String: reference, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), created.Version)
	assert.False(t, created.SelectedOptionID.Valid)
	assert.Equal(t, reference, created.ReferenceAnswer.String)
}
