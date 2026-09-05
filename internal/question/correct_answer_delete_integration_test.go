//go:build integration

package question

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestCorrectAnswerDeletionInvalidatesDerivedResults(t *testing.T) {
	for _, path := range []string{"direct", "option cascade", "question cascade"} {
		t.Run(path, func(t *testing.T) {
			store, pool := newQuestionIntegrationStore(t)
			ctx := t.Context()
			var questionID, oldOption, retainedOption uuid.UUID
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO questions(content,type) VALUES ('delete test','CHOICE') RETURNING id`).Scan(&questionID))
			t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM questions WHERE id=$1`, questionID) })
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO options(question_id,content,label) VALUES ($1,'old','A') RETURNING id`, questionID).Scan(&oldOption))
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO options(question_id,content,label) VALUES ($1,'retained','B') RETURNING id`, questionID).Scan(&retainedOption))
			correct, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{QuestionID: questionID, Type: "CHOICE", SelectedOptionID: nullableUUID(&oldOption)})
			require.NoError(t, err)
			require.Equal(t, int64(1), correct.Version)
			ids := make(map[string]uuid.UUID)
			for _, method := range []string{"DETERMINISTIC", "MANUAL"} {
				var userID uuid.UUID
				require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(email,name,roles) VALUES ($1,'delete test',ARRAY['STUDENT']::user_role[]) RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID))
				t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID) })
				experimentID := newAnswerTestExperiment(t, pool, userID)
				answer, err := store.CreateAnswer(ctx, CreateAnswerParams{QuestionID: questionID, UserID: userID, ExperimentID: experimentID, SelectedOptionID: nullableUUID(&retainedOption)})
				require.NoError(t, err)
				ids[method] = answer.ID
				_, err = store.CreateAnswerResult(ctx, CreateAnswerResultParams{AnswerID: answer.ID, Status: "GRADED", Method: pgtype.Text{String: method, Valid: true}, IsCorrect: pgtype.Bool{Valid: true}, GradedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}, CorrectAnswerVersion: pgtype.Int8{Int64: 1, Valid: true}})
				require.NoError(t, err)
			}
			read := func(method string) FindAnswerResultByQuestionAndIDRow {
				t.Helper()
				row, err := store.FindAnswerResultByQuestionAndID(ctx, FindAnswerResultByQuestionAndIDParams{QuestionID: questionID, AnswerID: ids[method]})
				require.NoError(t, err)
				return row
			}
			manual := read("MANUAL")
			// A rolled-back deletion must also roll back result invalidation.
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, `DELETE FROM correct_answers WHERE question_id=$1`, questionID)
			require.NoError(t, err)
			require.NoError(t, tx.Rollback(ctx))
			require.Equal(t, "GRADED", read("DETERMINISTIC").GradingStatus.String)
			switch path {
			case "direct":
				_, err = pool.Exec(ctx, `DELETE FROM correct_answers WHERE question_id=$1`, questionID)
			case "option cascade":
				err = store.DeleteOption(ctx, oldOption)
			case "question cascade":
				err = store.DeleteQuestion(ctx, questionID)
			}
			require.NoError(t, err)
			_, err = store.GetCorrectAnswer(ctx, questionID)
			require.ErrorIs(t, err, pgx.ErrNoRows)
			if path == "question cascade" {
				var count int
				require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM answer_results WHERE answer_id=ANY($1)`, []uuid.UUID{ids["DETERMINISTIC"], ids["MANUAL"]}).Scan(&count))
				require.Zero(t, count)
				return
			}
			invalidated := read("DETERMINISTIC")
			require.Equal(t, "PENDING", invalidated.GradingStatus.String)
			require.Equal(t, "DETERMINISTIC", invalidated.GradingMethod.String)
			require.False(t, invalidated.IsCorrect.Valid)
			require.False(t, invalidated.GradedAt.Valid)
			require.False(t, invalidated.CorrectAnswerVersion.Valid)
			// Compare the persisted MANUAL fields (current joined version changes).
			unchanged := read("MANUAL")
			unchanged.CurrentCorrectAnswerVersion = manual.CurrentCorrectAnswerVersion
			require.Equal(t, manual, unchanged)
			created, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{QuestionID: questionID, Type: "CHOICE", SelectedOptionID: nullableUUID(&retainedOption)})
			require.NoError(t, err)
			require.Equal(t, int64(1), created.Version)
			require.Equal(t, "PENDING", created.AnswerResultSyncStatus)
			require.Equal(t, "PENDING", read("DETERMINISTIC").GradingStatus.String)
			require.False(t, read("DETERMINISTIC").IsCorrect.Valid)
			count, err := store.SynchronizeAnswerResults(ctx, questionID, created.Version)
			require.NoError(t, err)
			require.Equal(t, 1, count)
			graded := read("DETERMINISTIC")
			require.Equal(t, "GRADED", graded.GradingStatus.String)
			require.Equal(t, pgtype.Bool{Bool: true, Valid: true}, graded.IsCorrect)
			require.True(t, graded.GradedAt.Valid)
			require.Equal(t, pgtype.Int8{Int64: 1, Valid: true}, graded.CorrectAnswerVersion)
			require.Equal(t, manual, read("MANUAL"))
		})
	}
}
