package question

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAnswerResultServiceGet_TableDriven(t *testing.T) {
	questionID := uuid.New()
	answerID := uuid.New()
	ownerID := uuid.New()
	otherID := uuid.New()
	gradedAt := time.Date(2026, time.August, 25, 8, 0, 0, 0, time.UTC)
	queryError := errors.New("query answer result")

	currentGraded := FindAnswerResultByQuestionAndIDRow{
		AnswerID: answerID, QuestionID: questionID, UserID: ownerID,
		GradingStatus:               pgtype.Text{String: "GRADED", Valid: true},
		GradingMethod:               pgtype.Text{String: "DETERMINISTIC", Valid: true},
		IsCorrect:                   pgtype.Bool{Bool: true, Valid: true},
		GradedAt:                    pgtype.Timestamptz{Time: gradedAt, Valid: true},
		CorrectAnswerVersion:        pgtype.Int8{Int64: 2, Valid: true},
		CurrentCorrectAnswerVersion: pgtype.Int8{Int64: 2, Valid: true},
	}

	tests := []struct {
		name        string
		row         FindAnswerResultByQuestionAndIDRow
		queryErr    error
		access      AnswerResultAccess
		showScore   bool
		wantErr     string
		wantStatus  GradingStatus
		wantVisible bool
		wantCorrect *bool
		wantMethod  *GradingMethod
	}{
		{
			name:   "student reads own current graded result",
			row:    currentGraded,
			access: AnswerResultAccess{ViewerKind: ResultViewerStudent, ViewerID: ownerID}, showScore: true,
			wantStatus: GradingStatusGraded, wantVisible: true,
			wantCorrect: resultBoolPointer(true), wantMethod: gradingMethodPointer(GradingMethodDeterministic),
		},
		{
			name:       "student own result obeys show score false",
			row:        currentGraded,
			access:     AnswerResultAccess{ViewerKind: ResultViewerStudent, ViewerID: ownerID},
			wantStatus: GradingStatusGraded,
			wantMethod: gradingMethodPointer(GradingMethodDeterministic),
		},
		{
			name:   "student reading another user answer is not found",
			row:    currentGraded,
			access: AnswerResultAccess{ViewerKind: ResultViewerStudent, ViewerID: otherID}, showScore: true,
			wantErr: "unable to find answers",
		},
		{
			name:       "management reads another user current result",
			row:        currentGraded,
			access:     AnswerResultAccess{ViewerKind: ResultViewerManagement, ViewerID: otherID},
			wantStatus: GradingStatusGraded, wantVisible: true,
			wantCorrect: resultBoolPointer(true), wantMethod: gradingMethodPointer(GradingMethodDeterministic),
		},
		{
			name: "pending omits correctness with provisional visible true",
			row: FindAnswerResultByQuestionAndIDRow{
				AnswerID: answerID, QuestionID: questionID, UserID: ownerID,
				GradingStatus: pgtype.Text{String: "PENDING", Valid: true},
			},
			access: AnswerResultAccess{ViewerKind: ResultViewerStudent, ViewerID: ownerID}, showScore: true,
			wantStatus: GradingStatusPending, wantVisible: true,
		},
		{
			name: "failed omits correctness with provisional visible true",
			row: FindAnswerResultByQuestionAndIDRow{
				AnswerID: answerID, QuestionID: questionID, UserID: ownerID,
				GradingStatus: pgtype.Text{String: "FAILED", Valid: true},
				IsCorrect:     pgtype.Bool{Bool: true, Valid: true},
			},
			access: AnswerResultAccess{ViewerKind: ResultViewerStudent, ViewerID: ownerID}, showScore: true,
			wantStatus: GradingStatusFailed, wantVisible: true,
		},
		{
			name: "stale graded result projects to pending and omits correctness",
			row: FindAnswerResultByQuestionAndIDRow{
				AnswerID: answerID, QuestionID: questionID, UserID: ownerID,
				GradingStatus:               pgtype.Text{String: "GRADED", Valid: true},
				GradingMethod:               pgtype.Text{String: "DETERMINISTIC", Valid: true},
				IsCorrect:                   pgtype.Bool{Bool: true, Valid: true},
				GradedAt:                    pgtype.Timestamptz{Time: gradedAt, Valid: true},
				CorrectAnswerVersion:        pgtype.Int8{Int64: 1, Valid: true},
				CurrentCorrectAnswerVersion: pgtype.Int8{Int64: 2, Valid: true},
			},
			access: AnswerResultAccess{ViewerKind: ResultViewerStudent, ViewerID: ownerID}, showScore: true,
			wantStatus: GradingStatusPending, wantVisible: true,
			wantMethod: gradingMethodPointer(GradingMethodDeterministic),
		},
		{
			name:   "missing persisted result projects to pending",
			row:    FindAnswerResultByQuestionAndIDRow{AnswerID: answerID, QuestionID: questionID, UserID: ownerID},
			access: AnswerResultAccess{ViewerKind: ResultViewerStudent, ViewerID: ownerID}, showScore: true,
			wantStatus: GradingStatusPending, wantVisible: true,
		},
		{
			name:     "query not found remains not found",
			queryErr: pgx.ErrNoRows,
			access:   AnswerResultAccess{ViewerKind: ResultViewerStudent, ViewerID: ownerID},
			wantErr:  "unable to find answers",
		},
		{
			name:     "query failure is propagated",
			queryErr: queryError,
			access:   AnswerResultAccess{ViewerKind: ResultViewerManagement, ViewerID: ownerID},
			wantErr:  "query answer result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{findAnswerResultFn: func(_ context.Context, arg FindAnswerResultByQuestionAndIDParams) (FindAnswerResultByQuestionAndIDRow, error) {
				assert.Equal(t, questionID, arg.QuestionID)
				assert.Equal(t, answerID, arg.AnswerID)
				return tt.row, tt.queryErr
			}}
			service := NewAnswerResultService(querier, resultVisibilityFunc(func(context.Context, uuid.UUID) (bool, error) { return tt.showScore, nil }), zap.NewNop())

			result, err := service.Get(t.Context(), questionID, answerID, tt.access)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, answerID, result.AnswerID)
			assert.Equal(t, questionID, result.QuestionID)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantVisible, result.ResultVisible)
			assert.Equal(t, tt.wantMethod, result.Method)
			if tt.wantCorrect == nil {
				assert.Nil(t, result.IsCorrect)
			} else {
				require.NotNil(t, result.IsCorrect)
				assert.Equal(t, *tt.wantCorrect, *result.IsCorrect)
			}
		})
	}
}

func resultBoolPointer(value bool) *bool { return &value }
