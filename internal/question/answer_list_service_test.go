package question

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAnswerServiceListByQuestion_TableDriven(t *testing.T) {
	questionID := uuid.New()
	tests := []struct {
		name           string
		input          AnswerListInput
		total          int64
		rows           []ListAnswersByQuestionAndExperimentPageRow
		wantErr        bool
		wantTotalPages int32
		wantHasNext    bool
		wantOffset     int32
	}{
		{name: "rejects zero page", input: AnswerListInput{ExperimentID: uuid.New(), Page: 0, PageSize: 20}, wantErr: true},
		{name: "rejects excessive page size", input: AnswerListInput{ExperimentID: uuid.New(), Page: 1, PageSize: 101}, wantErr: true},
		{name: "empty page", input: AnswerListInput{ExperimentID: uuid.New(), Page: 1, PageSize: 20}},
		{name: "first of two pages", input: AnswerListInput{ExperimentID: uuid.New(), Page: 1, PageSize: 20}, total: 21, wantTotalPages: 2, wantHasNext: true},
		{name: "second page offset", input: AnswerListInput{ExperimentID: uuid.New(), Page: 2, PageSize: 20}, total: 21, wantTotalPages: 2, wantOffset: 20},
		{
			name:  "projects current graded row",
			input: AnswerListInput{ExperimentID: uuid.New(), Page: 1, PageSize: 20},
			total: 1,
			rows: []ListAnswersByQuestionAndExperimentPageRow{{
				ID: uuid.New(), QuestionID: questionID, UserID: uuid.New(),
				GradingStatus:               pgtype.Text{String: "GRADED", Valid: true},
				GradingMethod:               pgtype.Text{String: "DETERMINISTIC", Valid: true},
				IsCorrect:                   pgtype.Bool{Bool: true, Valid: true},
				CorrectAnswerVersion:        pgtype.Int8{Int64: 4, Valid: true},
				CurrentCorrectAnswerVersion: pgtype.Int8{Int64: 4, Valid: true},
			}},
			wantTotalPages: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{ID: questionID, Type: "CHOICE"}, nil
				},
				listAnswersFn: func(_ context.Context, arg ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error) {
					assert.Equal(t, tt.wantOffset, arg.PageOffset)
					return tt.rows, nil
				},
				countAnswersFn: func(context.Context, uuid.UUID) (int64, error) { return tt.total, nil },
			}
			optionService := NewOptionService(querier, zap.NewNop())
			questionService := NewQuestionService(querier, optionService, zap.NewNop())
			service := NewAnswerService(querier, questionService, zap.NewNop())

			page, err := service.ListByQuestion(context.Background(), questionID, tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotalPages, page.TotalPages)
			assert.Equal(t, tt.wantHasNext, page.HasNextPage)
			assert.Equal(t, int32(tt.total), page.TotalItems)
			if len(tt.rows) > 0 {
				require.Len(t, page.Items, 1)
				assert.Equal(t, GradingStatusGraded, page.Items[0].GradingStatus)
				require.NotNil(t, page.Items[0].IsCorrect)
				assert.True(t, *page.Items[0].IsCorrect)
			}
		})
	}
}
