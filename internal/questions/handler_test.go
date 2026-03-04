package questions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

type fakeQuerier struct {
	listQuestionFn          func(ctx context.Context) ([]Question, error)
	getQuestionFn           func(ctx context.Context, id uuid.UUID) (Question, error)
	createQuestionFn        func(ctx context.Context, arg CreateQuestionParams) (Question, error)
	updateQuestionFn        func(ctx context.Context, arg UpdateQuestionParams) (Question, error)
	deleteQuestionFn        func(ctx context.Context, id uuid.UUID) error
	getOptionFn             func(ctx context.Context, id uuid.UUID) (Option, error)
	listOptionsByQuestionFn func(ctx context.Context, questionID uuid.UUID) ([]Option, error)
	createOptionFn          func(ctx context.Context, arg CreateOptionParams) (Option, error)
	updateOptionFn          func(ctx context.Context, arg UpdateOptionParams) (Option, error)
	deleteOptionFn          func(ctx context.Context, id uuid.UUID) error

	createOptionCalls []CreateOptionParams
	deleteOptionCalls []uuid.UUID
}

func (f *fakeQuerier) ListQuestion(ctx context.Context) ([]Question, error) {
	if f.listQuestionFn != nil {
		return f.listQuestionFn(ctx)
	}
	return nil, nil
}

func (f *fakeQuerier) GetQuestion(ctx context.Context, id uuid.UUID) (Question, error) {
	if f.getQuestionFn != nil {
		return f.getQuestionFn(ctx, id)
	}
	return Question{}, nil
}

func (f *fakeQuerier) CreateQuestion(ctx context.Context, arg CreateQuestionParams) (Question, error) {
	if f.createQuestionFn != nil {
		return f.createQuestionFn(ctx, arg)
	}
	return Question{}, nil
}

func (f *fakeQuerier) UpdateQuestion(ctx context.Context, arg UpdateQuestionParams) (Question, error) {
	if f.updateQuestionFn != nil {
		return f.updateQuestionFn(ctx, arg)
	}
	return Question{}, nil
}

func (f *fakeQuerier) DeleteQuestion(ctx context.Context, id uuid.UUID) error {
	if f.deleteQuestionFn != nil {
		return f.deleteQuestionFn(ctx, id)
	}
	return nil
}

func (f *fakeQuerier) GetOption(ctx context.Context, id uuid.UUID) (Option, error) {
	if f.getOptionFn != nil {
		return f.getOptionFn(ctx, id)
	}
	return Option{}, nil
}

func (f *fakeQuerier) ListOptionsByQuestion(ctx context.Context, questionID uuid.UUID) ([]Option, error) {
	if f.listOptionsByQuestionFn != nil {
		return f.listOptionsByQuestionFn(ctx, questionID)
	}
	return nil, nil
}

func (f *fakeQuerier) CreateOption(ctx context.Context, arg CreateOptionParams) (Option, error) {
	f.createOptionCalls = append(f.createOptionCalls, arg)
	if f.createOptionFn != nil {
		return f.createOptionFn(ctx, arg)
	}
	return Option{}, nil
}

func (f *fakeQuerier) UpdateOption(ctx context.Context, arg UpdateOptionParams) (Option, error) {
	if f.updateOptionFn != nil {
		return f.updateOptionFn(ctx, arg)
	}
	return Option{}, nil
}

func (f *fakeQuerier) DeleteOption(ctx context.Context, id uuid.UUID) error {
	f.deleteOptionCalls = append(f.deleteOptionCalls, id)
	if f.deleteOptionFn != nil {
		return f.deleteOptionFn(ctx, id)
	}
	return nil
}

func newTestMux(q *fakeQuerier) *http.ServeMux {
	logger := zap.NewNop()
	questionService := NewQuestionService(q, logger)
	optionService := NewOptionService(q, logger)
	handler := NewHandler(questionService, optionService, logger)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, nil)
	return mux
}

func TestHandlerList_TableDriven(t *testing.T) {
	choiceID := uuid.New()
	textID := uuid.New()
	optionID := uuid.New()

	tests := []struct {
		name       string
		querier    *fakeQuerier
		wantStatus int
		assertBody func(t *testing.T, body string)
	}{
		{
			name: "returns text and choice questions",
			querier: &fakeQuerier{
				listQuestionFn: func(context.Context) ([]Question, error) {
					return []Question{
						{ID: textID, Type: "TEXT", Content: "text question"},
						{ID: choiceID, Type: "CHOICE", Content: "choice question"},
					}, nil
				},
				listOptionsByQuestionFn: func(_ context.Context, questionID uuid.UUID) ([]Option, error) {
					if questionID != choiceID {
						return nil, nil
					}
					return []Option{{ID: optionID, QuestionID: choiceID, Label: "A", Content: "option A"}}, nil
				},
			},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, body string) {
				t.Helper()
				var got []map[string]any
				if err := json.Unmarshal([]byte(body), &got); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}
				if len(got) != 2 {
					t.Fatalf("want 2 questions, got %d", len(got))
				}
				if _, ok := got[0]["options"]; ok {
					t.Fatalf("TEXT question should not include options")
				}
				opts, ok := got[1]["options"].([]any)
				if !ok || len(opts) != 1 {
					t.Fatalf("CHOICE question should include 1 option")
				}
			},
		},
		{
			name: "returns internal error when list fails",
			querier: &fakeQuerier{listQuestionFn: func(context.Context) ([]Question, error) {
				return nil, errors.New("boom")
			}},
			wantStatus: http.StatusInternalServerError,
			assertBody: func(t *testing.T, body string) {
				t.Helper()
				if !strings.Contains(body, "Internal Server Error") {
					t.Fatalf("expected problem response, got: %s", body)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/questions", nil)

			newTestMux(tt.querier).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d", tt.wantStatus, rec.Code)
			}
			tt.assertBody(t, rec.Body.String())
		})
	}
}

func TestHandlerGet_TableDriven(t *testing.T) {
	id := uuid.New()
	optID := uuid.New()

	tests := []struct {
		name       string
		path       string
		querier    *fakeQuerier
		wantStatus int
		assertBody func(t *testing.T, body string)
	}{
		{
			name:       "invalid uuid",
			path:       "/api/questions/not-a-uuid",
			querier:    &fakeQuerier{},
			wantStatus: http.StatusBadRequest,
			assertBody: func(t *testing.T, body string) {
				t.Helper()
				if !strings.Contains(body, "Validation Problem") {
					t.Fatalf("expected validation problem, got: %s", body)
				}
			},
		},
		{
			name: "not found",
			path: "/api/questions/" + id.String(),
			querier: &fakeQuerier{getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
				return Question{}, pgx.ErrNoRows
			}},
			wantStatus: http.StatusNotFound,
			assertBody: func(t *testing.T, body string) {
				t.Helper()
				if !strings.Contains(body, "Not Found") {
					t.Fatalf("expected not found problem, got: %s", body)
				}
			},
		},
		{
			name: "choice question with options",
			path: "/api/questions/" + id.String(),
			querier: &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{ID: id, Type: "CHOICE", Content: "q"}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: optID, QuestionID: id, Label: "A", Content: "opt"}}, nil
				},
			},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, body string) {
				t.Helper()
				var got map[string]any
				if err := json.Unmarshal([]byte(body), &got); err != nil {
					t.Fatalf("failed to decode body: %v", err)
				}
				opts, ok := got["options"].([]any)
				if !ok || len(opts) != 1 {
					t.Fatalf("expected one option")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)

			newTestMux(tt.querier).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d", tt.wantStatus, rec.Code)
			}
			tt.assertBody(t, rec.Body.String())
		})
	}
}

func TestHandlerCreate_TableDriven(t *testing.T) {
	questionID := uuid.New()
	choiceID := uuid.New()
	choiceOptID := uuid.New()

	tests := []struct {
		name            string
		body            string
		querier         *fakeQuerier
		wantStatus      int
		wantCreateCalls int
	}{
		{
			name:            "invalid payload",
			body:            `{}`,
			querier:         &fakeQuerier{},
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name: "create text question",
			body: `{"type":"TEXT","content":"text answer"}`,
			querier: &fakeQuerier{createQuestionFn: func(context.Context, CreateQuestionParams) (Question, error) {
				return Question{ID: questionID, Type: "TEXT", Content: "text answer"}, nil
			}},
			wantStatus:      http.StatusCreated,
			wantCreateCalls: 0,
		},
		{
			name: "create choice question with options",
			body: `{"type":"CHOICE","content":"pick","options":[{"label":"A","content":"aaa"},{"label":"B","content":"bbb"}]}`,
			querier: &fakeQuerier{
				createQuestionFn: func(context.Context, CreateQuestionParams) (Question, error) {
					return Question{ID: choiceID, Type: "CHOICE", Content: "pick"}, nil
				},
				createOptionFn: func(_ context.Context, arg CreateOptionParams) (Option, error) {
					return Option{ID: choiceOptID, QuestionID: arg.QuestionID, Label: arg.Label, Content: arg.Content}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: choiceOptID, QuestionID: choiceID, Label: "A", Content: "aaa"}}, nil
				},
			},
			wantStatus:      http.StatusCreated,
			wantCreateCalls: 2,
		},
		{
			name: "choice options empty triggers validation problem",
			body: `{"type":"CHOICE","content":"pick","options":[]}`,
			querier: &fakeQuerier{createQuestionFn: func(context.Context, CreateQuestionParams) (Question, error) {
				return Question{ID: questionID, Type: "CHOICE", Content: "pick"}, nil
			}},
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/questions", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			newTestMux(tt.querier).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if got := len(tt.querier.createOptionCalls); got != tt.wantCreateCalls {
				t.Fatalf("create option calls mismatch: want %d got %d", tt.wantCreateCalls, got)
			}
		})
	}
}

func TestHandlerUpdate_TableDriven(t *testing.T) {
	qid := uuid.New()
	existingOpt1 := uuid.New()
	existingOpt2 := uuid.New()

	tests := []struct {
		name            string
		path            string
		body            string
		querier         *fakeQuerier
		wantStatus      int
		wantDeleteCalls int
		wantCreateCalls int
	}{
		{
			name:            "invalid uuid",
			path:            "/api/questions/not-a-uuid",
			body:            `{"type":"TEXT","content":"updated"}`,
			querier:         &fakeQuerier{},
			wantStatus:      http.StatusBadRequest,
			wantDeleteCalls: 0,
			wantCreateCalls: 0,
		},
		{
			name: "question not found",
			path: "/api/questions/" + qid.String(),
			body: `{"type":"TEXT","content":"updated"}`,
			querier: &fakeQuerier{getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
				return Question{}, pgx.ErrNoRows
			}},
			wantStatus:      http.StatusNotFound,
			wantDeleteCalls: 0,
			wantCreateCalls: 0,
		},
		{
			name: "update to text deletes existing options",
			path: "/api/questions/" + qid.String(),
			body: `{"type":"TEXT","content":"updated"}`,
			querier: &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{ID: qid, Type: "CHOICE", Content: "old"}, nil
				},
				updateQuestionFn: func(_ context.Context, arg UpdateQuestionParams) (Question, error) {
					return Question{ID: arg.ID, Type: arg.Type, Content: arg.Content}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: existingOpt1, QuestionID: qid}, {ID: existingOpt2, QuestionID: qid}}, nil
				},
			},
			wantStatus:      http.StatusOK,
			wantDeleteCalls: 2,
			wantCreateCalls: 0,
		},
		{
			name: "update choice replaces options",
			path: "/api/questions/" + qid.String(),
			body: `{"type":"CHOICE","content":"updated","options":[{"label":"A","content":"new"}]}`,
			querier: &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{ID: qid, Type: "CHOICE", Content: "old"}, nil
				},
				updateQuestionFn: func(_ context.Context, arg UpdateQuestionParams) (Question, error) {
					return Question{ID: arg.ID, Type: arg.Type, Content: arg.Content}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: existingOpt1, QuestionID: qid}}, nil
				},
			},
			wantStatus:      http.StatusOK,
			wantDeleteCalls: 1,
			wantCreateCalls: 1,
		},
		{
			name: "choice empty options rejected",
			path: "/api/questions/" + qid.String(),
			body: `{"type":"CHOICE","content":"updated","options":[]}`,
			querier: &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{ID: qid, Type: "CHOICE", Content: "old"}, nil
				},
				updateQuestionFn: func(_ context.Context, arg UpdateQuestionParams) (Question, error) {
					return Question{ID: arg.ID, Type: arg.Type, Content: arg.Content}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return nil, nil
				},
			},
			wantStatus:      http.StatusBadRequest,
			wantDeleteCalls: 0,
			wantCreateCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			newTestMux(tt.querier).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if got := len(tt.querier.deleteOptionCalls); got != tt.wantDeleteCalls {
				t.Fatalf("delete option calls mismatch: want %d got %d", tt.wantDeleteCalls, got)
			}
			if got := len(tt.querier.createOptionCalls); got != tt.wantCreateCalls {
				t.Fatalf("create option calls mismatch: want %d got %d", tt.wantCreateCalls, got)
			}
		})
	}
}

func TestHandlerDelete_TableDriven(t *testing.T) {
	qid := uuid.New()

	tests := []struct {
		name       string
		path       string
		querier    *fakeQuerier
		wantStatus int
	}{
		{
			name:       "invalid uuid",
			path:       "/api/questions/not-a-uuid",
			querier:    &fakeQuerier{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "question not found",
			path: "/api/questions/" + qid.String(),
			querier: &fakeQuerier{getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
				return Question{}, pgx.ErrNoRows
			}},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "delete failed",
			path: "/api/questions/" + qid.String(),
			querier: &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{ID: qid, Type: "TEXT", Content: "q"}, nil
				},
				deleteQuestionFn: func(context.Context, uuid.UUID) error {
					return errors.New("delete error")
				},
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "delete success",
			path: "/api/questions/" + qid.String(),
			querier: &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{ID: qid, Type: "TEXT", Content: "q"}, nil
				},
			},
			wantStatus: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)

			newTestMux(tt.querier).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}
