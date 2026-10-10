//go:build integration

package question

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnswerResultPersistenceAndManagementList(t *testing.T) {
	store, pool := newQuestionIntegrationStore(t)
	ctx := t.Context()
	marker := uuid.NewString()

	var userID, otherUserID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (email, name, roles) VALUES ($1, 'Answer Result User', ARRAY['STUDENT']::user_role[]) RETURNING id`,
		marker+"-first@example.com",
	).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (email, name, roles) VALUES ($1, 'Other Answer Result User', ARRAY['STUDENT']::user_role[]) RETURNING id`,
		marker+"-second@example.com",
	).Scan(&otherUserID))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{userID, otherUserID})
	})

	var questionID, optionID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO questions (content, type) VALUES ('choice', 'CHOICE') RETURNING id`,
	).Scan(&questionID))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM questions WHERE id = $1`, questionID)
	})
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO options (question_id, content, label) VALUES ($1, 'first', 'A') RETURNING id`,
		questionID,
	).Scan(&optionID))

	correctAnswer, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{
		QuestionID:       questionID,
		Type:             "CHOICE",
		SelectedOptionID: pgtype.UUID{Bytes: optionID, Valid: true},
	})
	require.NoError(t, err)
	experimentID := newAnswerTestExperiment(t, pool, userID)

	olderAnswer, err := store.CreateAnswer(ctx, CreateAnswerParams{
		ExperimentID:     experimentID,
		QuestionID:       questionID,
		UserID:           userID,
		SelectedOptionID: pgtype.UUID{Bytes: optionID, Valid: true},
	})
	require.NoError(t, err)
	newerAnswer, err := store.CreateAnswer(ctx, CreateAnswerParams{
		ExperimentID:     experimentID,
		QuestionID:       questionID,
		UserID:           otherUserID,
		SelectedOptionID: pgtype.UUID{Bytes: optionID, Valid: true},
	})
	require.NoError(t, err)
	newerCreatedAt := time.Now().UTC().Truncate(time.Microsecond)
	olderCreatedAt := newerCreatedAt.Add(-time.Minute)
	_, err = pool.Exec(ctx, `UPDATE answers SET created_at = $1 WHERE id = $2`, olderCreatedAt, olderAnswer.ID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE answers SET created_at = $1 WHERE id = $2`, newerCreatedAt, newerAnswer.ID)
	require.NoError(t, err)

	gradedAt := time.Now().UTC().Truncate(time.Microsecond)
	result, err := store.CreateAnswerResult(ctx, CreateAnswerResultParams{
		AnswerID:             newerAnswer.ID,
		Status:               "GRADED",
		Method:               pgtype.Text{String: "DETERMINISTIC", Valid: true},
		IsCorrect:            pgtype.Bool{Bool: true, Valid: true},
		GradedAt:             pgtype.Timestamptz{Time: gradedAt, Valid: true},
		CorrectAnswerVersion: pgtype.Int8{Int64: correctAnswer.Version, Valid: true},
	})
	require.NoError(t, err)
	assert.Equal(t, newerAnswer.ID, result.AnswerID)
	assert.True(t, result.IsCorrect.Bool)

	total, err := store.CountAnswersByQuestionAndExperiment(ctx, CountAnswersByQuestionAndExperimentParams{QuestionID: questionID, ExperimentID: experimentID})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)

	newestPage, err := store.ListAnswersByQuestionAndExperimentPage(ctx, ListAnswersByQuestionAndExperimentPageParams{
		ExperimentID: experimentID,
		QuestionID:   questionID,
		PageSize:     1,
	})
	require.NoError(t, err)
	require.Len(t, newestPage, 1)
	assert.Equal(t, newerAnswer.ID, newestPage[0].ID)
	assert.Equal(t, otherUserID, newestPage[0].UserID)
	assert.Equal(t, "GRADED", newestPage[0].GradingStatus.String)
	assert.Equal(t, "DETERMINISTIC", newestPage[0].GradingMethod.String)
	assert.True(t, newestPage[0].IsCorrect.Bool)
	assert.Equal(t, correctAnswer.Version, newestPage[0].CorrectAnswerVersion.Int64)
	assert.Equal(t, correctAnswer.Version, newestPage[0].CurrentCorrectAnswerVersion.Int64)

	olderPage, err := store.ListAnswersByQuestionAndExperimentPage(ctx, ListAnswersByQuestionAndExperimentPageParams{
		ExperimentID: experimentID,
		QuestionID:   questionID,
		PageOffset:   1,
		PageSize:     1,
	})
	require.NoError(t, err)
	require.Len(t, olderPage, 1)
	assert.Equal(t, olderAnswer.ID, olderPage[0].ID)
	assert.Equal(t, userID, olderPage[0].UserID)
}

func TestAnswerResultDatabaseConstraints_TableDriven(t *testing.T) {
	_, pool := newQuestionIntegrationStore(t)
	ctx := t.Context()
	marker := uuid.NewString()

	var userID, questionID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (email, name, roles) VALUES ($1, 'Constraint User', ARRAY['STUDENT']::user_role[]) RETURNING id`,
		marker+"@example.com",
	).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO questions (content, type) VALUES ('text', 'TEXT') RETURNING id`,
	).Scan(&questionID))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM questions WHERE id = $1`, questionID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	createAnswer := func(t *testing.T) uuid.UUID {
		t.Helper()
		var answerID uuid.UUID
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO answers (question_id, user_id, experiment_id, text_answer) VALUES ($1, $2, $3, 'answer') RETURNING id`,
			questionID, userID, newAnswerTestExperiment(t, pool, userID),
		).Scan(&answerID))
		return answerID
	}

	tests := []struct {
		name    string
		status  string
		method  *string
		result  *bool
		graded  bool
		version *int64
	}{
		{name: "pending cannot expose correctness", status: "PENDING", result: boolPointer(true)},
		{name: "graded requires correctness", status: "GRADED", method: textPointer("DETERMINISTIC"), graded: true, version: int64Pointer(1)},
		{name: "graded requires graded at", status: "GRADED", method: textPointer("DETERMINISTIC"), result: boolPointer(true), version: int64Pointer(1)},
		{name: "deterministic graded requires correct answer version", status: "GRADED", method: textPointer("DETERMINISTIC"), result: boolPointer(true), graded: true},
		{name: "rejects unknown method", status: "GRADED", method: textPointer("UNKNOWN"), result: boolPointer(true), graded: true, version: int64Pointer(1)},
		{name: "rejects nonpositive version", status: "GRADED", method: textPointer("DETERMINISTIC"), result: boolPointer(true), graded: true, version: int64Pointer(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answerID := createAnswer(t)
			var gradedAt *time.Time
			if tt.graded {
				now := time.Now().UTC()
				gradedAt = &now
			}
			_, err := pool.Exec(ctx, `
				INSERT INTO answer_results (answer_id, status, method, is_correct, graded_at, correct_answer_version)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, answerID, tt.status, tt.method, tt.result, gradedAt, tt.version)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			assert.Equal(t, "23514", pgErr.Code)
		})
	}

	manualAnswerID := createAnswer(t)
	_, err := pool.Exec(ctx, `
		INSERT INTO answer_results (answer_id, status, method, is_correct, graded_at)
		VALUES ($1, 'GRADED', 'MANUAL', true, NOW())
	`, manualAnswerID)
	require.NoError(t, err, "the deterministic-version invariant must not impose unresolved MANUAL version semantics")
}

func TestRegradeAnswerResultRejectsStaleTargetVersion(t *testing.T) {
	store, pool := newQuestionIntegrationStore(t)
	ctx := t.Context()
	marker := uuid.NewString()

	var userID, questionID, firstOptionID, secondOptionID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (email, name, roles) VALUES ($1, 'Regrade User', ARRAY['STUDENT']::user_role[]) RETURNING id`,
		marker+"@example.com",
	).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO questions (content, type) VALUES ('choice', 'CHOICE') RETURNING id`,
	).Scan(&questionID))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM questions WHERE id = $1`, questionID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO options (question_id, content, label) VALUES ($1, 'first', 'A') RETURNING id`, questionID,
	).Scan(&firstOptionID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO options (question_id, content, label) VALUES ($1, 'second', 'B') RETURNING id`, questionID,
	).Scan(&secondOptionID))

	versionOne, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{
		QuestionID:       questionID,
		Type:             "CHOICE",
		SelectedOptionID: pgtype.UUID{Bytes: firstOptionID, Valid: true},
	})
	require.NoError(t, err)
	answer, err := store.CreateAnswer(ctx, CreateAnswerParams{
		ExperimentID:     newAnswerTestExperiment(t, pool, userID),
		QuestionID:       questionID,
		UserID:           userID,
		SelectedOptionID: pgtype.UUID{Bytes: firstOptionID, Valid: true},
	})
	require.NoError(t, err)
	gradedAt := time.Now().UTC()
	_, err = store.CreateAnswerResult(ctx, CreateAnswerResultParams{
		AnswerID:             answer.ID,
		Status:               "GRADED",
		Method:               pgtype.Text{String: "DETERMINISTIC", Valid: true},
		IsCorrect:            pgtype.Bool{Bool: true, Valid: true},
		GradedAt:             pgtype.Timestamptz{Time: gradedAt, Valid: true},
		CorrectAnswerVersion: pgtype.Int8{Int64: versionOne.Version, Valid: true},
	})
	require.NoError(t, err)

	versionTwo, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{
		QuestionID:       questionID,
		Type:             "CHOICE",
		SelectedOptionID: pgtype.UUID{Bytes: secondOptionID, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), versionTwo.Version)

	newerResult, err := store.RegradeAnswerResultForVersion(ctx, RegradeAnswerResultForVersionParams{
		AnswerID:      answer.ID,
		Status:        "GRADED",
		Method:        pgtype.Text{String: "DETERMINISTIC", Valid: true},
		IsCorrect:     pgtype.Bool{Bool: false, Valid: true},
		GradedAt:      pgtype.Timestamptz{Time: gradedAt.Add(time.Second), Valid: true},
		TargetVersion: versionTwo.Version,
	})
	require.NoError(t, err)
	assert.Equal(t, versionTwo.Version, newerResult.CorrectAnswerVersion.Int64)

	_, err = store.RegradeAnswerResultForVersion(ctx, RegradeAnswerResultForVersionParams{
		AnswerID:      answer.ID,
		Status:        "GRADED",
		Method:        pgtype.Text{String: "DETERMINISTIC", Valid: true},
		IsCorrect:     pgtype.Bool{Bool: true, Valid: true},
		GradedAt:      pgtype.Timestamptz{Time: gradedAt.Add(2 * time.Second), Valid: true},
		TargetVersion: versionOne.Version,
	})
	assert.ErrorIs(t, err, pgx.ErrNoRows)

	var persistedVersion int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT correct_answer_version FROM answer_results WHERE answer_id = $1`, answer.ID,
	).Scan(&persistedVersion))
	assert.Equal(t, versionTwo.Version, persistedVersion)
}

func boolPointer(value bool) *bool     { return &value }
func textPointer(value string) *string { return &value }
func int64Pointer(value int64) *int64  { return &value }

func newAnswerTestExperiment(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO experiments
		(created_by, name, configuration, status, scheduled_start_at, scheduled_end_at)
		VALUES ($1, 'answer test', '{"maxAttempts":1,"allowRetry":false,"showScore":true,"showExplanations":false,"gradingMode":"AUTOMATIC","correctAnswerReleaseMode":"NEVER"}',
		'ACTIVE', now() - interval '1 hour', now() + interval '1 hour') RETURNING id`, userID).Scan(&id))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM answers WHERE experiment_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM experiments WHERE id = $1`, id)
	})
	return id
}
