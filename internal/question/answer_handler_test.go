package question

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"

	"sciedu-backend/internal/auth"
)

type fakeAnswerQuerier struct {
	*fakeQuerier

	createAnswerFn func(ctx context.Context, arg CreateAnswerParams) (Answer, error)
	listAnswersFn  func(ctx context.Context, arg ListAnswersByQuestionForUserParams) ([]Answer, error)

	createAnswerCalls []CreateAnswerParams
}

func (f *fakeAnswerQuerier) CreateAnswer(ctx context.Context, arg CreateAnswerParams) (Answer, error) {
	f.createAnswerCalls = append(f.createAnswerCalls, arg)
	if f.createAnswerFn != nil {
		return f.createAnswerFn(ctx, arg)
	}
	return Answer{}, nil
}

func (f *fakeAnswerQuerier) ListAnswersByQuestionForUser(ctx context.Context, arg ListAnswersByQuestionForUserParams) ([]Answer, error) {
	if f.listAnswersFn != nil {
		return f.listAnswersFn(ctx, arg)
	}
	return nil, nil
}

func newAnswerTestMux(f *fakeAnswerQuerier) *http.ServeMux {
	logger := zap.NewNop()
	optionService := NewOptionService(f, logger)
	questionService := NewQuestionService(f, optionService, logger)
	answerService := NewAnswerService(f, questionService, logger)
	handler := NewAnswerHandler(answerService, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, nil)
	return mux
}

func choiceQuestion(id uuid.UUID) func(context.Context, uuid.UUID) (Question, error) {
	return func(context.Context, uuid.UUID) (Question, error) {
		return Question{ID: id, Type: "CHOICE", Content: "pick one"}, nil
	}
}

func textQuestion(id uuid.UUID) func(context.Context, uuid.UUID) (Question, error) {
	return func(context.Context, uuid.UUID) (Question, error) {
		return Question{ID: id, Type: "TEXT", Content: "explain"}, nil
	}
}

func TestAnswerHandlerSubmit_TableDriven(t *testing.T) {
	questionID := uuid.New()
	userID := uuid.New()
	optionID := uuid.New()
	otherOptionID := uuid.New()

	tests := []struct {
		name            string
		body            string
		querier         *fakeAnswerQuerier
		withUser        bool
		wantStatus      int
		wantCreateCalls int
	}{
		{
			name: "choice answer created",
			body: `{"selectedOptionId":"` + optionID.String() + `"}`,
			querier: &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
				getQuestionFn: choiceQuestion(questionID),
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: optionID, QuestionID: questionID, Label: "A"}}, nil
				},
			}},
			withUser:        true,
			wantStatus:      http.StatusCreated,
			wantCreateCalls: 1,
		},
		{
			name: "text answer created",
			body: `{"textAnswer":"my thoughts"}`,
			querier: &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
				getQuestionFn: textQuestion(questionID),
			}},
			withUser:        true,
			wantStatus:      http.StatusCreated,
			wantCreateCalls: 1,
		},
		{
			name: "question not found returns 404",
			body: `{"textAnswer":"hi"}`,
			querier: &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{}, pgx.ErrNoRows
				},
			}},
			withUser:        true,
			wantStatus:      http.StatusNotFound,
			wantCreateCalls: 0,
		},
		{
			name: "choice missing option returns 400",
			body: `{"textAnswer":"not an option"}`,
			querier: &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
				getQuestionFn: choiceQuestion(questionID),
			}},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name: "choice option not belonging returns 400",
			body: `{"selectedOptionId":"` + otherOptionID.String() + `"}`,
			querier: &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
				getQuestionFn: choiceQuestion(questionID),
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: optionID, QuestionID: questionID, Label: "A"}}, nil
				},
			}},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name: "choice with text returns 400",
			body: `{"selectedOptionId":"` + optionID.String() + `","textAnswer":"extra"}`,
			querier: &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
				getQuestionFn: choiceQuestion(questionID),
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: optionID, QuestionID: questionID, Label: "A"}}, nil
				},
			}},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name: "text missing text returns 400",
			body: `{}`,
			querier: &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
				getQuestionFn: textQuestion(questionID),
			}},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name: "text with option returns 400",
			body: `{"textAnswer":"hi","selectedOptionId":"` + optionID.String() + `"}`,
			querier: &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
				getQuestionFn: textQuestion(questionID),
			}},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name: "missing user returns 401",
			body: `{"textAnswer":"hi"}`,
			querier: &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
				getQuestionFn: textQuestion(questionID),
			}},
			withUser:        false,
			wantStatus:      http.StatusUnauthorized,
			wantCreateCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/questions/"+questionID.String()+"/answers", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.withUser {
				req = req.WithContext(auth.ContextWithUserID(req.Context(), userID))
			}

			newAnswerTestMux(tt.querier).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if got := len(tt.querier.createAnswerCalls); got != tt.wantCreateCalls {
				t.Fatalf("create answer calls mismatch: want %d got %d", tt.wantCreateCalls, got)
			}
		})
	}
}

func TestAnswerHandlerSubmit_PassesUserAndConversions(t *testing.T) {
	questionID := uuid.New()
	userID := uuid.New()
	optionID := uuid.New()

	querier := &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{
		getQuestionFn: choiceQuestion(questionID),
		listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
			return []Option{{ID: optionID, QuestionID: questionID, Label: "A"}}, nil
		},
	}}
	querier.createAnswerFn = func(_ context.Context, arg CreateAnswerParams) (Answer, error) {
		return Answer{
			ID:               uuid.New(),
			QuestionID:       arg.QuestionID,
			UserID:           arg.UserID,
			SelectedOptionID: arg.SelectedOptionID,
			CreatedAt:        pgtype.Timestamptz{Valid: true},
		}, nil
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/questions/"+questionID.String()+"/answers", strings.NewReader(`{"selectedOptionId":"`+optionID.String()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithUserID(req.Context(), userID))

	newAnswerTestMux(querier).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("want 201 got %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(querier.createAnswerCalls) != 1 {
		t.Fatalf("expected one create call")
	}
	call := querier.createAnswerCalls[0]
	if call.UserID != userID {
		t.Fatalf("user id not propagated: want %s got %s", userID, call.UserID)
	}
	if call.QuestionID != questionID {
		t.Fatalf("question id not propagated: want %s got %s", questionID, call.QuestionID)
	}
	if !call.SelectedOptionID.Valid || uuid.UUID(call.SelectedOptionID.Bytes) != optionID {
		t.Fatalf("selected option not converted to valid pgtype.UUID")
	}
	if call.TextAnswer.Valid {
		t.Fatalf("text answer should be null for choice answer")
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if body["selectedOptionId"] != optionID.String() {
		t.Fatalf("response selectedOptionId mismatch: %v", body["selectedOptionId"])
	}
	if body["textAnswer"] != nil {
		t.Fatalf("response textAnswer should be null, got %v", body["textAnswer"])
	}
}

func TestAnswerHandlerList_TableDriven(t *testing.T) {
	questionID := uuid.New()
	userID := uuid.New()

	tests := []struct {
		name       string
		querier    *fakeAnswerQuerier
		withUser   bool
		wantStatus int
		wantLen    int
	}{
		{
			name: "returns answers newest first",
			querier: &fakeAnswerQuerier{
				fakeQuerier: &fakeQuerier{},
				listAnswersFn: func(_ context.Context, arg ListAnswersByQuestionForUserParams) ([]Answer, error) {
					if arg.UserID != userID || arg.QuestionID != questionID {
						t.Errorf("querier received wrong filter: %+v", arg)
					}
					return []Answer{
						{ID: uuid.New(), QuestionID: questionID, TextAnswer: pgtype.Text{String: "newer", Valid: true}},
						{ID: uuid.New(), QuestionID: questionID, TextAnswer: pgtype.Text{String: "older", Valid: true}},
					}, nil
				},
			},
			withUser:   true,
			wantStatus: http.StatusOK,
			wantLen:    2,
		},
		{
			name:       "no answers returns empty array",
			querier:    &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{}},
			withUser:   true,
			wantStatus: http.StatusOK,
			wantLen:    0,
		},
		{
			name:       "missing user returns 401",
			querier:    &fakeAnswerQuerier{fakeQuerier: &fakeQuerier{}},
			withUser:   false,
			wantStatus: http.StatusUnauthorized,
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/questions/"+questionID.String()+"/answers", nil)
			if tt.withUser {
				req = req.WithContext(auth.ContextWithUserID(req.Context(), userID))
			}

			newAnswerTestMux(tt.querier).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			var got []map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to decode body: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("want %d answers, got %d", tt.wantLen, len(got))
			}
			if tt.wantLen == 2 && got[0]["textAnswer"] != "newer" {
				t.Fatalf("order not preserved: %v", got[0]["textAnswer"])
			}
		})
	}
}
