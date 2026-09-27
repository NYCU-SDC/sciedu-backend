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

func TestAnswerSynchronizationEligibility(t *testing.T) {
	for _, tt := range []struct {
		name, questionType, mode, resultMethod string
		correct, stale, eligible               bool
		pending                                bool
	}{
		{name: "missing result", questionType: "CHOICE", mode: "AUTOMATIC", correct: true, eligible: true},
		{name: "stale graded", questionType: "CHOICE", mode: "AUTOMATIC", correct: true, stale: true, resultMethod: "DETERMINISTIC", eligible: true},
		{name: "current graded", questionType: "CHOICE", mode: "AUTOMATIC", correct: true, resultMethod: "DETERMINISTIC"},
		{name: "manual result preserved", questionType: "CHOICE", mode: "AUTOMATIC", correct: true, stale: true, resultMethod: "MANUAL"},
		{name: "manual experiment", questionType: "CHOICE", mode: "MANUAL", correct: true},
		{name: "text not automatically gradable", questionType: "TEXT", mode: "AUTOMATIC", correct: true},
		{name: "no correct answer", questionType: "CHOICE", mode: "AUTOMATIC"},
		{name: "manual pending in automatic experiment", questionType: "CHOICE", mode: "AUTOMATIC", correct: true, resultMethod: "MANUAL", pending: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store, pool := newQuestionIntegrationStore(t)
			ctx := t.Context()
			var userID, questionID, optionID uuid.UUID
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(email,name,roles) VALUES ($1,'sync',ARRAY['STUDENT']::user_role[]) RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID))
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO questions(content,type) VALUES ('sync',$1) RETURNING id`, tt.questionType).Scan(&questionID))
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), `DELETE FROM questions WHERE id=$1`, questionID)
				_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
			})
			experimentID := newAnswerTestExperiment(t, pool, userID)
			_, err := pool.Exec(ctx, `UPDATE experiments SET configuration=jsonb_set(configuration,'{gradingMode}',to_jsonb($2::text)) WHERE id=$1`, experimentID, tt.mode)
			require.NoError(t, err)
			request := CreateAnswerParams{QuestionID: questionID, UserID: userID, ExperimentID: experimentID}
			correct := UpsertCorrectAnswerParams{QuestionID: questionID, Type: tt.questionType}
			if tt.questionType == "CHOICE" {
				require.NoError(t, pool.QueryRow(ctx, `INSERT INTO options(question_id,content,label) VALUES ($1,'a','A') RETURNING id`, questionID).Scan(&optionID))
				request.SelectedOptionID = pgtype.UUID{Bytes: optionID, Valid: true}
				correct.SelectedOptionID = request.SelectedOptionID
			} else {
				request.TextAnswer = pgtype.Text{String: "text", Valid: true}
				correct.ReferenceAnswer = request.TextAnswer
			}
			answer, err := store.CreateAnswer(ctx, request)
			require.NoError(t, err)
			if tt.correct {
				_, err = store.UpsertCorrectAnswer(ctx, correct)
				require.NoError(t, err)
			}
			version := int64(1)
			if tt.stale {
				version = 2
				_, err = pool.Exec(ctx, `UPDATE correct_answers SET version=2 WHERE question_id=$1`, questionID)
				require.NoError(t, err)
			}
			if tt.pending {
				_, err = store.CreateAnswerResult(ctx, CreateAnswerResultParams{AnswerID: answer.ID, Status: "PENDING", Method: pgtype.Text{String: tt.resultMethod, Valid: true}})
				require.NoError(t, err)
			} else if tt.resultMethod != "" {
				_, err = store.CreateAnswerResult(ctx, CreateAnswerResultParams{AnswerID: answer.ID, Status: "GRADED",
					Method: pgtype.Text{String: tt.resultMethod, Valid: true}, IsCorrect: pgtype.Bool{Bool: true, Valid: true},
					GradedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, CorrectAnswerVersion: pgtype.Int8{Int64: 1, Valid: true}})
				require.NoError(t, err)
			}
			rows, err := store.ListAnswerSynchronizationCandidates(ctx, ListAnswerSynchronizationCandidatesParams{QuestionID: questionID, TargetVersion: version, BatchSize: 100})
			require.NoError(t, err)
			if !tt.eligible {
				require.Empty(t, rows)
				_, err := store.RegradeAnswerResultForVersion(ctx, RegradeAnswerResultForVersionParams{
					AnswerID: answer.ID, Status: "GRADED", Method: pgtype.Text{String: "DETERMINISTIC", Valid: true},
					IsCorrect: pgtype.Bool{Bool: true, Valid: true}, GradedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, TargetVersion: version,
				})
				require.ErrorIs(t, err, pgx.ErrNoRows)
				return
			}
			require.Len(t, rows, 1)
			count, err := store.SynchronizeAnswerResults(ctx, questionID, version)
			require.NoError(t, err)
			require.Equal(t, 1, count)
			persisted, err := store.FindAnswerResultByQuestionAndID(ctx, FindAnswerResultByQuestionAndIDParams{QuestionID: questionID, AnswerID: answer.ID})
			require.NoError(t, err)
			require.Equal(t, version, persisted.CorrectAnswerVersion.Int64)
			synced, err := store.GetCorrectAnswer(ctx, questionID)
			require.NoError(t, err)
			require.Equal(t, "SYNCED", synced.AnswerResultSyncStatus)
			count, err = store.SynchronizeAnswerResults(ctx, questionID, version)
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}
