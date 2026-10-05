package question

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestInitialSubmissionGrading(t *testing.T) {
	optionID := uuid.New()
	readErr := errors.New("database unavailable")
	for _, tt := range []struct {
		name, questionType                string
		mode                              SubmissionGradingMode
		lookupErr                         error
		wantLookup, wantGraded, wantError bool
		method                            string
	}{
		{"automatic choice without correct answer", "CHOICE", SubmissionGradingModeAutomatic, pgx.ErrNoRows, true, false, false, "DETERMINISTIC"},
		{"automatic choice with correct answer", "CHOICE", SubmissionGradingModeAutomatic, nil, true, true, false, "DETERMINISTIC"},
		{"lookup failure is not missing correct answer", "CHOICE", SubmissionGradingModeAutomatic, readErr, true, false, true, ""},
		{"manual choice stays pending", "CHOICE", SubmissionGradingModeManual, nil, false, false, false, "MANUAL"},
		{"manual text stays pending", "TEXT", SubmissionGradingModeManual, nil, false, false, false, "MANUAL"},
		{"automatic text stays pending", "TEXT", SubmissionGradingModeAutomatic, nil, false, false, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := DecideSubmissionGrading(tt.questionType, tt.mode)
			require.NoError(t, err)
			called := false
			reader := &fakeQuerier{getCorrectAnswerFn: func(context.Context, uuid.UUID) (CorrectAnswer, error) {
				called = true
				return CorrectAnswer{Version: 1, SelectedOptionID: pgtype.UUID{Bytes: optionID, Valid: true}}, tt.lookupErr
			}}
			grading, err := initialSubmissionGrading(t.Context(), reader, AnswerSubmissionCommand{
				Answer: AnswerRequest{SelectedOptionID: &optionID}, Grading: decision,
			}, time.Now().UTC())
			require.Equal(t, tt.wantLookup, called)
			if tt.wantError {
				require.ErrorIs(t, err, readErr)
				return
			}
			require.NoError(t, err)
			params := createAnswerResultParams(uuid.New(), grading)
			require.Equal(t, tt.method, params.Method.String)
			if tt.wantGraded {
				require.Equal(t, "GRADED", params.Status)
				require.True(t, params.IsCorrect.Valid && params.IsCorrect.Bool)
				require.Equal(t, int64(1), params.CorrectAnswerVersion.Int64)
				return
			}
			require.Equal(t, "PENDING", params.Status)
			require.False(t, params.IsCorrect.Valid)
			require.False(t, params.GradedAt.Valid)
			require.False(t, params.CorrectAnswerVersion.Valid)
		})
	}
}
