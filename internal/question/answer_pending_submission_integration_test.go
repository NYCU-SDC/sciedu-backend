//go:build integration

package question

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSubmissionWithoutCorrectAnswerThenSynchronization(t *testing.T) {
	for _, mode := range []SubmissionGradingMode{SubmissionGradingModeAutomatic, SubmissionGradingModeManual} {
		t.Run(string(mode), func(t *testing.T) {
			store, pool := newQuestionIntegrationStore(t)
			ctx := t.Context()
			var userID, questionID, optionID, courseID, pageID uuid.UUID
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(email,name,roles) VALUES ($1,'pending submission',ARRAY['STUDENT']::user_role[]) RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID))
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO questions(content,type) VALUES ('pending choice','CHOICE') RETURNING id`).Scan(&questionID))
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), `DELETE FROM questions WHERE id=$1`, questionID)
				_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
			})
			experimentID := newAnswerTestExperiment(t, pool, userID)
			_, err := pool.Exec(ctx, `UPDATE experiments SET configuration=jsonb_set(configuration,'{gradingMode}',to_jsonb($2::text)) WHERE id=$1`, experimentID, string(mode))
			require.NoError(t, err)
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO options(question_id,content,label) VALUES ($1,'a','A') RETURNING id`, questionID).Scan(&optionID))
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO courses(code,title,status) VALUES ($1,'pending submission','PUBLISHED') RETURNING id`, uuid.NewString()).Scan(&courseID))
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), `DELETE FROM pages WHERE course_id=$1`, courseID)
				_, _ = pool.Exec(context.Background(), `DELETE FROM courses WHERE id=$1`, courseID)
			})
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO pages(course_id,title,display_order) VALUES ($1,'page',0) RETURNING id`, courseID).Scan(&pageID))
			_, err = pool.Exec(ctx, `INSERT INTO page_blocks(page_id,question_id,display_order,required) VALUES ($1,$2,0,true)`, pageID, questionID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO experiment_courses(experiment_id,course_id) VALUES ($1,$2)`, experimentID, courseID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO experiment_participants(experiment_id,user_id) VALUES ($1,$2)`, experimentID, userID)
			require.NoError(t, err)
			questions := NewQuestionService(store, NewOptionService(store, zap.NewNop()), zap.NewNop())
			submission := NewAnswerSubmissionOrchestrator(NewAnswerService(store, questions, zap.NewNop()), store, store)
			answer, err := submission.Submit(ctx, AnswerRequest{QuestionID: questionID, UserID: userID, SelectedOptionID: &optionID})
			require.NoError(t, err)
			require.Equal(t, experimentID, answer.ExperimentID)
			lookup := FindAnswerResultByQuestionAndIDParams{QuestionID: questionID, AnswerID: answer.ID}
			pending, err := store.FindAnswerResultByQuestionAndID(ctx, lookup)
			require.NoError(t, err)
			require.True(t, pending.GradingStatus.Valid) // A real row, not nil-result projection.
			require.Equal(t, "PENDING", pending.GradingStatus.String)
			require.False(t, pending.IsCorrect.Valid)
			require.False(t, pending.GradedAt.Valid)
			require.False(t, pending.CorrectAnswerVersion.Valid)
			candidates, err := store.ListAnswerSynchronizationCandidates(ctx, ListAnswerSynchronizationCandidatesParams{QuestionID: questionID, TargetVersion: 1, BatchSize: 100})
			require.NoError(t, err)
			require.Empty(t, candidates)
			correct, err := store.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{QuestionID: questionID, Type: "CHOICE", SelectedOptionID: pgtype.UUID{Bytes: optionID, Valid: true}})
			require.NoError(t, err)
			count, err := store.SynchronizeAnswerResults(ctx, questionID, correct.Version)
			require.NoError(t, err)
			result, err := store.FindAnswerResultByQuestionAndID(ctx, lookup)
			require.NoError(t, err)
			if mode == SubmissionGradingModeManual {
				require.Zero(t, count)
				require.Equal(t, pending, resultWithoutCurrentVersion(result))
				return
			}
			require.Equal(t, 1, count)
			require.Equal(t, "GRADED", result.GradingStatus.String)
			require.True(t, result.IsCorrect.Valid && result.IsCorrect.Bool)
			require.Equal(t, correct.Version, result.CorrectAnswerVersion.Int64)
			require.True(t, result.GradedAt.Valid)
		})
	}
}

func resultWithoutCurrentVersion(row FindAnswerResultByQuestionAndIDRow) FindAnswerResultByQuestionAndIDRow {
	row.CurrentCorrectAnswerVersion = pgtype.Int8{}
	return row
}
