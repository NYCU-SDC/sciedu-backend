package question

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

func TestCorrectAnswerServiceUpsert_TableDriven(t *testing.T) {
	questionID := uuid.New()
	optionID := uuid.New()
	otherQuestionID := uuid.New()
	tooLong := strings.Repeat("字", maxReferenceAnswerLength+1)
	referenceAnswer := "reference"

	tests := []struct {
		name        string
		question    Question
		option      Option
		request     CorrectAnswerRequest
		wantErr     bool
		wantUpserts int
	}{
		{
			name:        "stores choice correct answer",
			question:    Question{ID: questionID, Type: "CHOICE"},
			option:      Option{ID: optionID, QuestionID: questionID},
			request:     CorrectAnswerRequest{Type: "CHOICE", SelectedOptionID: &optionID},
			wantUpserts: 1,
		},
		{
			name:        "stores text reference answer",
			question:    Question{ID: questionID, Type: "TEXT"},
			request:     CorrectAnswerRequest{Type: "TEXT", ReferenceAnswer: &referenceAnswer},
			wantUpserts: 1,
		},
		{
			name:     "rejects mismatched type",
			question: Question{ID: questionID, Type: "TEXT"},
			request:  CorrectAnswerRequest{Type: "CHOICE", SelectedOptionID: &optionID},
			wantErr:  true,
		},
		{
			name:     "rejects option from another question",
			question: Question{ID: questionID, Type: "CHOICE"},
			option:   Option{ID: optionID, QuestionID: otherQuestionID},
			request:  CorrectAnswerRequest{Type: "CHOICE", SelectedOptionID: &optionID},
			wantErr:  true,
		},
		{
			name:     "rejects choice without selected option",
			question: Question{ID: questionID, Type: "CHOICE"},
			request:  CorrectAnswerRequest{Type: "CHOICE"},
			wantErr:  true,
		},
		{
			name:     "rejects text without reference answer",
			question: Question{ID: questionID, Type: "TEXT"},
			request:  CorrectAnswerRequest{Type: "TEXT"},
			wantErr:  true,
		},
		{
			name:     "rejects overlong unicode reference answer",
			question: Question{ID: questionID, Type: "TEXT"},
			request:  CorrectAnswerRequest{Type: "TEXT", ReferenceAnswer: &tooLong},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return tt.question, nil
				},
				getOptionFn: func(context.Context, uuid.UUID) (Option, error) {
					return tt.option, nil
				},
				upsertCorrectAnswerFn: func(_ context.Context, arg UpsertCorrectAnswerParams) (CorrectAnswer, error) {
					return CorrectAnswer{
						QuestionID:             arg.QuestionID,
						Type:                   arg.Type,
						SelectedOptionID:       arg.SelectedOptionID,
						ReferenceAnswer:        arg.ReferenceAnswer,
						Version:                1,
						AnswerResultSyncStatus: "PENDING",
					}, nil
				},
			}
			optionService := NewOptionService(querier, zap.NewNop())
			questionService := NewQuestionService(querier, optionService, zap.NewNop())
			service := NewCorrectAnswerService(querier, questionService, zap.NewNop())

			_, err := service.Upsert(context.Background(), questionID, tt.request)
			if tt.wantErr && !errors.Is(err, errInvalidCorrectAnswerPayload) {
				t.Fatalf("expected invalid correct answer payload, got %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got := len(querier.upsertCorrectAnswerCalls); got != tt.wantUpserts {
				t.Fatalf("upsert calls mismatch: want %d got %d", tt.wantUpserts, got)
			}
		})
	}
}

func TestCorrectAnswerServiceGet_TableDriven(t *testing.T) {
	questionID := uuid.New()
	tests := []struct {
		name             string
		getQuestionErr   error
		getCorrectAnswer func(context.Context, uuid.UUID) (CorrectAnswer, error)
		wantErr          bool
	}{
		{
			name: "returns stored correct answer",
			getCorrectAnswer: func(context.Context, uuid.UUID) (CorrectAnswer, error) {
				return CorrectAnswer{
					QuestionID:      questionID,
					Type:            "TEXT",
					ReferenceAnswer: pgtype.Text{String: "reference", Valid: true},
					Version:         1,
				}, nil
			},
		},
		{name: "question does not exist", getQuestionErr: errors.New("missing"), wantErr: true},
		{
			name: "correct answer does not exist",
			getCorrectAnswer: func(context.Context, uuid.UUID) (CorrectAnswer, error) {
				return CorrectAnswer{}, errors.New("missing")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					if tt.getQuestionErr != nil {
						return Question{}, tt.getQuestionErr
					}
					return Question{ID: questionID, Type: "TEXT"}, nil
				},
				getCorrectAnswerFn: tt.getCorrectAnswer,
			}
			optionService := NewOptionService(querier, zap.NewNop())
			questionService := NewQuestionService(querier, optionService, zap.NewNop())
			service := NewCorrectAnswerService(querier, questionService, zap.NewNop())

			_, err := service.Get(context.Background(), questionID)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
