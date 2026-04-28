package question

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	created UpsertRequest
	answer  SubmitAnswerRequest
}

func (f *fakeStore) List(ctx context.Context) ([]Question, error) { return nil, nil }
func (f *fakeStore) Get(ctx context.Context, id uuid.UUID) (Question, error) {
	return Question{ID: id}, nil
}
func (f *fakeStore) Create(ctx context.Context, req UpsertRequest) (Question, error) {
	f.created = req
	return Question{ID: uuid.New(), Type: req.Type, Content: req.Content}, nil
}
func (f *fakeStore) Update(ctx context.Context, id uuid.UUID, req UpsertRequest) (Question, error) {
	f.created = req
	return Question{ID: id, Type: req.Type, Content: req.Content}, nil
}
func (f *fakeStore) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (f *fakeStore) ListAnswers(ctx context.Context, questionID uuid.UUID) ([]Answer, error) {
	return nil, nil
}
func (f *fakeStore) CreateAnswer(ctx context.Context, questionID uuid.UUID, req SubmitAnswerRequest) (Answer, error) {
	f.answer = req
	return Answer{ID: uuid.New(), QuestionID: questionID}, nil
}

func TestServiceCreateValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     UpsertRequest
		wantErr bool
	}{
		{name: "choice with options", req: UpsertRequest{Type: TypeChoice, Content: "pick", Options: []OptionInput{{Label: "A", Content: "one"}}}},
		{name: "text without options", req: UpsertRequest{Type: TypeText, Content: "why"}},
		{name: "missing content", req: UpsertRequest{Type: TypeText}, wantErr: true},
		{name: "choice without options", req: UpsertRequest{Type: TypeChoice, Content: "pick"}, wantErr: true},
		{name: "text with options", req: UpsertRequest{Type: TypeText, Content: "why", Options: []OptionInput{{Label: "A", Content: "one"}}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(&fakeStore{})
			_, err := service.Create(context.Background(), tt.req)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestServiceSubmitAnswerValidation(t *testing.T) {
	text := "because"
	optionID := uuid.New()
	tests := []struct {
		name    string
		req     SubmitAnswerRequest
		wantErr bool
	}{
		{name: "selected option", req: SubmitAnswerRequest{SelectedOptionID: &optionID}},
		{name: "text answer", req: SubmitAnswerRequest{TextAnswer: &text}},
		{name: "empty", req: SubmitAnswerRequest{}, wantErr: true},
		{name: "both", req: SubmitAnswerRequest{SelectedOptionID: &optionID, TextAnswer: &text}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(&fakeStore{})
			_, err := service.SubmitAnswer(context.Background(), uuid.New(), tt.req)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
