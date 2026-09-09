package question

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"sciedu-backend/internal/auth"
)

type fakeQuerier struct {
	scopeFn                 func(context.Context, IsAnswerManagementScopeReachableParams) (bool, error)
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
	createAnswerFn          func(ctx context.Context, arg CreateAnswerParams) (Answer, error)
	listAnswersFn           func(ctx context.Context, arg ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error)
	countAnswersFn          func(ctx context.Context, questionID uuid.UUID) (int64, error)
	findAnswerResultFn      func(ctx context.Context, arg FindAnswerResultByQuestionAndIDParams) (FindAnswerResultByQuestionAndIDRow, error)
	getCorrectAnswerFn      func(ctx context.Context, questionID uuid.UUID) (CorrectAnswer, error)
	upsertCorrectAnswerFn   func(ctx context.Context, arg UpsertCorrectAnswerParams) (CorrectAnswer, error)

	createQuestionCalls      []CreateQuestionParams
	updateQuestionCalls      []UpdateQuestionParams
	createOptionCalls        []CreateOptionParams
	deleteOptionCalls        []uuid.UUID
	createAnswerCalls        []CreateAnswerParams
	upsertCorrectAnswerCalls []UpsertCorrectAnswerParams
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
	f.createQuestionCalls = append(f.createQuestionCalls, arg)
	if f.createQuestionFn != nil {
		return f.createQuestionFn(ctx, arg)
	}
	return Question{}, nil
}

func (f *fakeQuerier) UpdateQuestion(ctx context.Context, arg UpdateQuestionParams) (Question, error) {
	f.updateQuestionCalls = append(f.updateQuestionCalls, arg)
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

func (f *fakeQuerier) CreateAnswer(ctx context.Context, arg CreateAnswerParams) (Answer, error) {
	f.createAnswerCalls = append(f.createAnswerCalls, arg)
	if f.createAnswerFn != nil {
		return f.createAnswerFn(ctx, arg)
	}
	return Answer{}, nil
}

func (f *fakeQuerier) ListAnswersByQuestionAndExperimentPage(ctx context.Context, arg ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error) {
	if f.listAnswersFn != nil {
		return f.listAnswersFn(ctx, arg)
	}
	return nil, nil
}

func (f *fakeQuerier) CountAnswersByQuestionAndExperiment(ctx context.Context, arg CountAnswersByQuestionAndExperimentParams) (int64, error) {
	if f.countAnswersFn != nil {
		return f.countAnswersFn(ctx, arg.QuestionID)
	}
	return 0, nil
}

func (f *fakeQuerier) IsAnswerManagementScopeReachable(ctx context.Context, arg IsAnswerManagementScopeReachableParams) (bool, error) {
	if f.scopeFn != nil {
		return f.scopeFn(ctx, arg)
	}
	_, err := f.GetQuestion(ctx, uuid.UUID(arg.QuestionID.Bytes))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (f *fakeQuerier) FindAnswerResultByQuestionAndID(
	ctx context.Context,
	arg FindAnswerResultByQuestionAndIDParams,
) (FindAnswerResultByQuestionAndIDRow, error) {
	if f.findAnswerResultFn != nil {
		return f.findAnswerResultFn(ctx, arg)
	}
	return FindAnswerResultByQuestionAndIDRow{}, pgx.ErrNoRows
}

func (f *fakeQuerier) GetCorrectAnswer(ctx context.Context, questionID uuid.UUID) (CorrectAnswer, error) {
	if f.getCorrectAnswerFn != nil {
		return f.getCorrectAnswerFn(ctx, questionID)
	}
	return CorrectAnswer{}, pgx.ErrNoRows
}

func (f *fakeQuerier) UpsertCorrectAnswer(ctx context.Context, arg UpsertCorrectAnswerParams) (CorrectAnswer, error) {
	f.upsertCorrectAnswerCalls = append(f.upsertCorrectAnswerCalls, arg)
	if f.upsertCorrectAnswerFn != nil {
		return f.upsertCorrectAnswerFn(ctx, arg)
	}
	return CorrectAnswer{}, nil
}

func (f *fakeQuerier) WithinTx(_ context.Context, fn func(QuestionQuerier, OptionQuerier) error) error {
	return fn(f, f)
}

func newTestMux(q *fakeQuerier) *http.ServeMux {
	logger := zap.NewNop()
	optionService := NewOptionService(q, logger)
	questionService := NewQuestionService(q, optionService, logger)
	answerService := NewAnswerService(q, questionService, logger)
	correctAnswerService := NewCorrectAnswerService(q, questionService, logger)
	handler := NewHandler(questionService, answerService, correctAnswerService, logger)
	handler.WithSubmission(NewAnswerSubmissionOrchestrator(answerService,
		&fakeSubmissionContextResolver{resolveFn: func(context.Context, uuid.UUID, uuid.UUID) (SubmissionContext, error) {
			return SubmissionContext{ExperimentID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), GradingMode: SubmissionGradingModeAutomatic}, nil
		}}, &fakeAnswerSubmissionTransaction{submitFn: func(ctx context.Context, command AnswerSubmissionCommand) (Answer, error) {
			return q.CreateAnswer(ctx, CreateAnswerParams{QuestionID: command.Answer.QuestionID, UserID: command.Answer.UserID,
				ExperimentID: command.Context.ExperimentID, SelectedOptionID: nullableUUID(command.Answer.SelectedOptionID), TextAnswer: nullableText(command.Answer.TextAnswer)})
		}}))

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, nil, nil)
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
		name              string
		body              string
		querier           *fakeQuerier
		wantStatus        int
		wantQuestionCalls int
		wantCreateCalls   int
	}{
		{
			name:              "invalid payload",
			body:              `{}`,
			querier:           &fakeQuerier{},
			wantStatus:        http.StatusBadRequest,
			wantQuestionCalls: 0,
			wantCreateCalls:   0,
		},
		{
			name: "create text question",
			body: `{"type":"TEXT","content":"text answer"}`,
			querier: &fakeQuerier{createQuestionFn: func(context.Context, CreateQuestionParams) (Question, error) {
				return Question{ID: questionID, Type: "TEXT", Content: "text answer"}, nil
			}},
			wantStatus:        http.StatusCreated,
			wantQuestionCalls: 1,
			wantCreateCalls:   0,
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
			wantStatus:        http.StatusCreated,
			wantQuestionCalls: 1,
			wantCreateCalls:   2,
		},
		{
			name: "choice options empty triggers validation problem",
			body: `{"type":"CHOICE","content":"pick","options":[]}`,
			querier: &fakeQuerier{createQuestionFn: func(context.Context, CreateQuestionParams) (Question, error) {
				return Question{ID: questionID, Type: "CHOICE", Content: "pick"}, nil
			}},
			wantStatus:        http.StatusBadRequest,
			wantQuestionCalls: 0,
			wantCreateCalls:   0,
		},
		{
			name: "choice duplicate labels rejected before writes",
			body: `{"type":"CHOICE","content":"pick","options":[{"label":"A","content":"aaa"},{"label":"A","content":"bbb"}]}`,
			querier: &fakeQuerier{createQuestionFn: func(context.Context, CreateQuestionParams) (Question, error) {
				return Question{ID: questionID, Type: "CHOICE", Content: "pick"}, nil
			}},
			wantStatus:        http.StatusBadRequest,
			wantQuestionCalls: 0,
			wantCreateCalls:   0,
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
			if got := len(tt.querier.createQuestionCalls); got != tt.wantQuestionCalls {
				t.Fatalf("create question calls mismatch: want %d got %d", tt.wantQuestionCalls, got)
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
		wantUpdateCalls int
		wantDeleteCalls int
		wantCreateCalls int
	}{
		{
			name:            "invalid uuid",
			path:            "/api/questions/not-a-uuid",
			body:            `{"type":"TEXT","content":"updated"}`,
			querier:         &fakeQuerier{},
			wantStatus:      http.StatusBadRequest,
			wantUpdateCalls: 0,
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
			wantUpdateCalls: 0,
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
					return Question{
						ID:      arg.ID,
						Type:    arg.Type,
						Content: arg.Content,
					}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: existingOpt1, QuestionID: qid}, {ID: existingOpt2, QuestionID: qid}}, nil
				},
			},
			wantStatus:      http.StatusOK,
			wantUpdateCalls: 1,
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
					return Question{
						ID:      arg.ID,
						Type:    arg.Type,
						Content: arg.Content,
					}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: existingOpt1, QuestionID: qid}}, nil
				},
			},
			wantStatus:      http.StatusOK,
			wantUpdateCalls: 1,
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
					return Question{
						ID:      arg.ID,
						Type:    arg.Type,
						Content: arg.Content,
					}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return nil, nil
				},
			},
			wantStatus:      http.StatusBadRequest,
			wantUpdateCalls: 0,
			wantDeleteCalls: 0,
			wantCreateCalls: 0,
		},
		{
			name: "choice duplicate labels rejected before replacing options",
			path: "/api/questions/" + qid.String(),
			body: `{"type":"CHOICE","content":"updated","options":[{"label":"A","content":"new"},{"label":"A","content":"duplicate"}]}`,
			querier: &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{ID: qid, Type: "CHOICE", Content: "old"}, nil
				},
				updateQuestionFn: func(_ context.Context, arg UpdateQuestionParams) (Question, error) {
					return Question{
						ID:      arg.ID,
						Type:    arg.Type,
						Content: arg.Content,
					}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: existingOpt1, QuestionID: qid}}, nil
				},
			},
			wantStatus:      http.StatusBadRequest,
			wantUpdateCalls: 0,
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
			if got := len(tt.querier.updateQuestionCalls); got != tt.wantUpdateCalls {
				t.Fatalf("update question calls mismatch: want %d got %d", tt.wantUpdateCalls, got)
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

func TestHandlerSubmitAnswer_TableDriven(t *testing.T) {
	questionID := uuid.New()
	userID := uuid.New()
	optionID := uuid.New()
	otherOptionID := uuid.New()

	tests := []struct {
		name            string
		body            string
		querier         *fakeQuerier
		withUser        bool
		wantStatus      int
		wantCreateCalls int
	}{
		{
			name: "choice answer created",
			body: `{"selectedOptionId":"` + optionID.String() + `"}`,
			querier: &fakeQuerier{
				getQuestionFn: choiceQuestion(questionID),
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: optionID, QuestionID: questionID, Label: "A"}}, nil
				},
			},
			withUser:        true,
			wantStatus:      http.StatusCreated,
			wantCreateCalls: 1,
		},
		{
			name:            "text answer created",
			body:            `{"textAnswer":"my thoughts"}`,
			querier:         &fakeQuerier{getQuestionFn: textQuestion(questionID)},
			withUser:        true,
			wantStatus:      http.StatusCreated,
			wantCreateCalls: 1,
		},
		{
			name: "question not found returns 404",
			body: `{"textAnswer":"hi"}`,
			querier: &fakeQuerier{getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
				return Question{}, pgx.ErrNoRows
			}},
			withUser:        true,
			wantStatus:      http.StatusNotFound,
			wantCreateCalls: 0,
		},
		{
			name:            "choice missing option returns 400",
			body:            `{"textAnswer":"not an option"}`,
			querier:         &fakeQuerier{getQuestionFn: choiceQuestion(questionID)},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name: "choice option not belonging returns 400",
			body: `{"selectedOptionId":"` + otherOptionID.String() + `"}`,
			querier: &fakeQuerier{
				getQuestionFn: choiceQuestion(questionID),
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: optionID, QuestionID: questionID, Label: "A"}}, nil
				},
			},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name: "choice with text returns 400",
			body: `{"selectedOptionId":"` + optionID.String() + `","textAnswer":"extra"}`,
			querier: &fakeQuerier{
				getQuestionFn: choiceQuestion(questionID),
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return []Option{{ID: optionID, QuestionID: questionID, Label: "A"}}, nil
				},
			},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name:            "text missing text returns 400",
			body:            `{}`,
			querier:         &fakeQuerier{getQuestionFn: textQuestion(questionID)},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name:            "text with option returns 400",
			body:            `{"textAnswer":"hi","selectedOptionId":"` + optionID.String() + `"}`,
			querier:         &fakeQuerier{getQuestionFn: textQuestion(questionID)},
			withUser:        true,
			wantStatus:      http.StatusBadRequest,
			wantCreateCalls: 0,
		},
		{
			name:            "missing user returns 401",
			body:            `{"textAnswer":"hi"}`,
			querier:         &fakeQuerier{getQuestionFn: textQuestion(questionID)},
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

			newTestMux(tt.querier).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if got := len(tt.querier.createAnswerCalls); got != tt.wantCreateCalls {
				t.Fatalf("create answer calls mismatch: want %d got %d", tt.wantCreateCalls, got)
			}
		})
	}
}

func TestHandlerSubmitAnswer_PassesUserAndConversions(t *testing.T) {
	questionID := uuid.New()
	userID := uuid.New()
	optionID := uuid.New()

	querier := &fakeQuerier{
		getQuestionFn: choiceQuestion(questionID),
		listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
			return []Option{{ID: optionID, QuestionID: questionID, Label: "A"}}, nil
		},
		createAnswerFn: func(_ context.Context, arg CreateAnswerParams) (Answer, error) {
			return Answer{
				ID:               uuid.New(),
				QuestionID:       arg.QuestionID,
				UserID:           arg.UserID,
				SelectedOptionID: arg.SelectedOptionID,
				CreatedAt:        pgtype.Timestamptz{Valid: true},
			}, nil
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/questions/"+questionID.String()+"/answers", strings.NewReader(`{"selectedOptionId":"`+optionID.String()+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithUserID(req.Context(), userID))

	newTestMux(querier).ServeHTTP(rec, req)

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
	if _, ok := body["textAnswer"]; ok {
		t.Fatalf("response textAnswer should be omitted for a choice answer, got %v", body["textAnswer"])
	}
}

func TestHandlerListAnswers_TableDriven(t *testing.T) {
	questionID := uuid.New()
	answerID := uuid.New()
	userID := uuid.New()
	otherAnswerID := uuid.New()
	otherUserID := uuid.New()
	gradedAt := time.Now().UTC()

	tests := []struct {
		name       string
		query      string
		querier    *fakeQuerier
		wantStatus int
		assertBody func(t *testing.T, body map[string]any)
	}{
		{
			name:  "returns paginated graded answer",
			query: "?page=2&pageSize=1",
			querier: &fakeQuerier{
				listAnswersFn: func(_ context.Context, arg ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error) {
					if arg.QuestionID != questionID || arg.PageOffset != 1 || arg.PageSize != 1 {
						t.Errorf("unexpected list params: %+v", arg)
					}
					return []ListAnswersByQuestionAndExperimentPageRow{
						{
							ID: answerID, QuestionID: questionID, UserID: userID,
							TextAnswer:                  pgtype.Text{String: "answer", Valid: true},
							CreatedAt:                   pgtype.Timestamptz{Time: gradedAt, Valid: true},
							GradingStatus:               pgtype.Text{String: "GRADED", Valid: true},
							GradingMethod:               pgtype.Text{String: "DETERMINISTIC", Valid: true},
							IsCorrect:                   pgtype.Bool{Bool: true, Valid: true},
							GradedAt:                    pgtype.Timestamptz{Time: gradedAt, Valid: true},
							CorrectAnswerVersion:        pgtype.Int8{Int64: 2, Valid: true},
							CurrentCorrectAnswerVersion: pgtype.Int8{Int64: 2, Valid: true},
						},
					}, nil
				},
				countAnswersFn: func(context.Context, uuid.UUID) (int64, error) { return 2, nil },
			},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				items := body["items"].([]any)
				item := items[0].(map[string]any)
				if item["gradingStatus"] != "GRADED" || item["gradingMethod"] != "DETERMINISTIC" || item["isCorrect"] != true {
					t.Fatalf("unexpected grading response: %v", item)
				}
				if body["totalPages"] != float64(2) || body["currentPage"] != float64(2) || body["hasNextPage"] != false {
					t.Fatalf("unexpected pagination response: %v", body)
				}
			},
		},
		{
			name: "returns multiple users newest first",
			querier: &fakeQuerier{
				listAnswersFn: func(context.Context, ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error) {
					return []ListAnswersByQuestionAndExperimentPageRow{
						{ID: answerID, QuestionID: questionID, UserID: userID, TextAnswer: pgtype.Text{String: "newer", Valid: true}},
						{ID: otherAnswerID, QuestionID: questionID, UserID: otherUserID, TextAnswer: pgtype.Text{String: "older", Valid: true}},
					}, nil
				},
				countAnswersFn: func(context.Context, uuid.UUID) (int64, error) { return 2, nil },
			},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				items := body["items"].([]any)
				if len(items) != 2 {
					t.Fatalf("expected answers from two users: %v", items)
				}
				newer := items[0].(map[string]any)
				older := items[1].(map[string]any)
				if newer["id"] != answerID.String() || newer["userId"] != userID.String() || newer["textAnswer"] != "newer" {
					t.Fatalf("unexpected newest answer: %v", newer)
				}
				if older["id"] != otherAnswerID.String() || older["userId"] != otherUserID.String() || older["textAnswer"] != "older" {
					t.Fatalf("unexpected older answer: %v", older)
				}
			},
		},
		{
			name: "stale result is pending and omits correctness",
			querier: &fakeQuerier{
				listAnswersFn: func(context.Context, ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error) {
					return []ListAnswersByQuestionAndExperimentPageRow{
						{
							ID: answerID, QuestionID: questionID, UserID: userID,
							GradingStatus:               pgtype.Text{String: "GRADED", Valid: true},
							IsCorrect:                   pgtype.Bool{Bool: false, Valid: true},
							CorrectAnswerVersion:        pgtype.Int8{Int64: 1, Valid: true},
							CurrentCorrectAnswerVersion: pgtype.Int8{Int64: 2, Valid: true},
						},
					}, nil
				},
				countAnswersFn: func(context.Context, uuid.UUID) (int64, error) { return 1, nil },
			},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				item := body["items"].([]any)[0].(map[string]any)
				if item["gradingStatus"] != "PENDING" {
					t.Fatalf("stale result must be pending: %v", item)
				}
				if _, ok := item["isCorrect"]; ok {
					t.Fatalf("stale result must omit isCorrect: %v", item)
				}
			},
		},
		{
			name: "omitted pagination uses defaults",
			querier: &fakeQuerier{
				listAnswersFn: func(_ context.Context, arg ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error) {
					if arg.PageOffset != 0 || arg.PageSize != 20 {
						t.Errorf("unexpected default pagination params: %+v", arg)
					}
					return nil, nil
				},
			},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if len(body["items"].([]any)) != 0 || body["totalItems"] != float64(0) {
					t.Fatalf("unexpected empty page: %v", body)
				}
			},
		},
		{
			name: "question not found returns 404",
			querier: &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{}, pgx.ErrNoRows
				},
				listAnswersFn: func(context.Context, ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error) {
					t.Error("querier should not be called when the question does not exist")
					return nil, nil
				},
			},
			wantStatus: http.StatusNotFound,
		},
		{name: "rejects invalid page", query: "?page=0", querier: &fakeQuerier{}, wantStatus: http.StatusBadRequest},
		{name: "rejects excessive page size", query: "?pageSize=101", querier: &fakeQuerier{}, wantStatus: http.StatusBadRequest},
		{name: "rejects empty page", query: "?page=", querier: &fakeQuerier{}, wantStatus: http.StatusBadRequest},
		{name: "rejects empty page size", query: "?pageSize=", querier: &fakeQuerier{}, wantStatus: http.StatusBadRequest},
		{name: "rejects non integer page", query: "?page=x", querier: &fakeQuerier{}, wantStatus: http.StatusBadRequest},
		{name: "rejects non integer page size", query: "?pageSize=x", querier: &fakeQuerier{}, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/questions/"+questionID.String()+"/answers"+tt.query, nil)
			values := req.URL.Query()
			values.Set("experimentId", uuid.NewString())
			req.URL.RawQuery = values.Encode()

			newTestMux(tt.querier).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantStatus, rec.Code, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to decode body: %v", err)
			}
			if tt.assertBody != nil {
				tt.assertBody(t, body)
			}
		})
	}
}

func TestBuildAnswerResultResponse_TableDriven(t *testing.T) {
	answerID := uuid.New()
	questionID := uuid.New()
	method := GradingMethodDeterministic
	correct := true
	gradedAt := time.Date(2026, time.August, 25, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		result     AnswerResultView
		wantFields map[string]any
		omitFields []string
	}{
		{
			name: "graded response includes visible correctness",
			result: AnswerResultView{
				AnswerID: answerID, QuestionID: questionID, Status: GradingStatusGraded,
				Method: &method, ResultVisible: true, IsCorrect: &correct, GradedAt: &gradedAt,
			},
			wantFields: map[string]any{"status": "GRADED", "method": "DETERMINISTIC", "resultVisible": true, "isCorrect": true},
		},
		{
			name: "pending response omits correctness",
			result: AnswerResultView{
				AnswerID: answerID, QuestionID: questionID, Status: GradingStatusPending,
				ResultVisible: true,
			},
			wantFields: map[string]any{"status": "PENDING", "resultVisible": true},
			omitFields: []string{"method", "isCorrect", "gradedAt"},
		},
		{
			name: "failed response omits correctness",
			result: AnswerResultView{
				AnswerID: answerID, QuestionID: questionID, Status: GradingStatusFailed,
				ResultVisible: true,
			},
			wantFields: map[string]any{"status": "FAILED", "resultVisible": true},
			omitFields: []string{"isCorrect"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(buildAnswerResultResponse(tt.result))
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(body, &decoded))
			assert.Equal(t, answerID.String(), decoded["answerId"])
			assert.Equal(t, questionID.String(), decoded["questionId"])
			for key, value := range tt.wantFields {
				assert.Equal(t, value, decoded[key], key)
			}
			for _, key := range tt.omitFields {
				assert.NotContains(t, decoded, key)
			}
		})
	}
}

type fakeRoleQuerier struct {
	roles []auth.Role
}

func (f fakeRoleQuerier) ActiveUserRoles(ctx context.Context, userID uuid.UUID) ([]auth.Role, error) {
	return f.roles, nil
}

func newAuthorizedMux(q *fakeQuerier, actorID uuid.UUID, roles []auth.Role) *http.ServeMux {
	logger := zap.NewNop()
	optionService := NewOptionService(q, logger)
	questionService := NewQuestionService(q, optionService, logger)
	answerService := NewAnswerService(q, questionService, logger)
	correctAnswerService := NewCorrectAnswerService(q, questionService, logger)
	handler := NewHandler(questionService, answerService, correctAnswerService, logger)
	handler.WithSubmission(NewAnswerSubmissionOrchestrator(answerService,
		&fakeSubmissionContextResolver{resolveFn: func(context.Context, uuid.UUID, uuid.UUID) (SubmissionContext, error) {
			return SubmissionContext{ExperimentID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), GradingMode: SubmissionGradingModeAutomatic}, nil
		}}, &fakeAnswerSubmissionTransaction{submitFn: func(ctx context.Context, command AnswerSubmissionCommand) (Answer, error) {
			return q.CreateAnswer(ctx, CreateAnswerParams{QuestionID: command.Answer.QuestionID, UserID: command.Answer.UserID,
				ExperimentID: command.Context.ExperimentID, SelectedOptionID: nullableUUID(command.Answer.SelectedOptionID), TextAnswer: nullableText(command.Answer.TextAnswer)})
		}}))

	set := middlewareutil.NewSet(func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(auth.ContextWithUserID(r.Context(), actorID)))
		}
	})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, set, auth.NewAuthorizer(fakeRoleQuerier{roles: roles}, nil))
	return mux
}

// TestHandlerListAnswers_Authorization exercises the real RequireAnyRole wiring:
// GET answers needs EXPERIMENTER/ADMIN, while POST answers stays open to any
// authenticated user.
func TestHandlerListAnswers_Authorization(t *testing.T) {
	questionID := uuid.New()
	actorID := uuid.New()

	newQuerier := func() *fakeQuerier {
		return &fakeQuerier{
			getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
				return Question{ID: questionID, Type: "TEXT", Content: "q"}, nil
			},
			listAnswersFn: func(context.Context, ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error) {
				return []ListAnswersByQuestionAndExperimentPageRow{{ID: uuid.New(), QuestionID: questionID, UserID: uuid.New(), TextAnswer: pgtype.Text{String: "a", Valid: true}}}, nil
			},
			countAnswersFn: func(context.Context, uuid.UUID) (int64, error) { return 1, nil },
		}
	}

	tests := []struct {
		name     string
		roles    []auth.Role
		method   string
		wantCode int
	}{
		{name: "student cannot read answers", roles: []auth.Role{auth.STUDENT}, method: http.MethodGet, wantCode: http.StatusForbidden},
		{name: "experimenter can read answers", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodGet, wantCode: http.StatusOK},
		{name: "admin can read answers", roles: []auth.Role{auth.ADMIN}, method: http.MethodGet, wantCode: http.StatusOK},
		{name: "student can still submit answers", roles: []auth.Role{auth.STUDENT}, method: http.MethodPost, wantCode: http.StatusCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newAuthorizedMux(newQuerier(), actorID, tt.roles)
			url := "/api/questions/" + questionID.String() + "/answers"

			var req *http.Request
			if tt.method == http.MethodPost {
				req = httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{"textAnswer":"my answer"}`))
			} else {
				req = httptest.NewRequest(http.MethodGet, url+"?experimentId="+uuid.NewString(), nil)
			}

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandlerCorrectAnswer_TableDriven(t *testing.T) {
	questionID := uuid.New()
	optionID := uuid.New()
	updatedAt := time.Date(2026, time.August, 18, 5, 14, 58, 0, time.UTC)
	longReference := strings.Repeat("a", maxReferenceAnswerLength+1)

	tests := []struct {
		name        string
		method      string
		body        string
		querier     *fakeQuerier
		wantStatus  int
		wantUpserts int
		assertBody  func(t *testing.T, body map[string]any)
	}{
		{
			name:   "gets choice correct answer",
			method: http.MethodGet,
			querier: &fakeQuerier{
				getQuestionFn: choiceQuestion(questionID),
				getCorrectAnswerFn: func(context.Context, uuid.UUID) (CorrectAnswer, error) {
					return CorrectAnswer{
						QuestionID:             questionID,
						Type:                   "CHOICE",
						SelectedOptionID:       pgtype.UUID{Bytes: optionID, Valid: true},
						Version:                1,
						AnswerResultSyncStatus: "PENDING",
						UpdatedAt:              pgtype.Timestamptz{Time: updatedAt, Valid: true},
					}, nil
				},
			},
			wantStatus: http.StatusOK,
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["selectedOptionId"] != optionID.String() || body["version"] != float64(1) {
					t.Fatalf("unexpected response: %v", body)
				}
				if _, ok := body["referenceAnswer"]; ok {
					t.Fatalf("choice response must omit referenceAnswer: %v", body)
				}
			},
		},
		{
			name:   "missing correct answer returns 404",
			method: http.MethodGet,
			querier: &fakeQuerier{
				getQuestionFn:      choiceQuestion(questionID),
				getCorrectAnswerFn: func(context.Context, uuid.UUID) (CorrectAnswer, error) { return CorrectAnswer{}, pgx.ErrNoRows },
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:   "upserts choice correct answer",
			method: http.MethodPut,
			body:   `{"type":"CHOICE","selectedOptionId":"` + optionID.String() + `"}`,
			querier: &fakeQuerier{
				getQuestionFn: choiceQuestion(questionID),
				getOptionFn: func(context.Context, uuid.UUID) (Option, error) {
					return Option{ID: optionID, QuestionID: questionID}, nil
				},
				upsertCorrectAnswerFn: func(_ context.Context, arg UpsertCorrectAnswerParams) (CorrectAnswer, error) {
					return CorrectAnswer{QuestionID: arg.QuestionID, Type: arg.Type, SelectedOptionID: arg.SelectedOptionID, Version: 1, AnswerResultSyncStatus: "PENDING"}, nil
				},
			},
			wantStatus:  http.StatusOK,
			wantUpserts: 1,
		},
		{
			name:   "upserts text correct answer",
			method: http.MethodPut,
			body:   `{"type":"TEXT","referenceAnswer":"reference"}`,
			querier: &fakeQuerier{
				getQuestionFn: textQuestion(questionID),
				upsertCorrectAnswerFn: func(_ context.Context, arg UpsertCorrectAnswerParams) (CorrectAnswer, error) {
					return CorrectAnswer{QuestionID: arg.QuestionID, Type: arg.Type, ReferenceAnswer: arg.ReferenceAnswer, Version: 2, AnswerResultSyncStatus: "PENDING"}, nil
				},
			},
			wantStatus:  http.StatusOK,
			wantUpserts: 1,
			assertBody: func(t *testing.T, body map[string]any) {
				t.Helper()
				if body["referenceAnswer"] != "reference" || body["version"] != float64(2) {
					t.Fatalf("unexpected response: %v", body)
				}
			},
		},
		{
			name:       "rejects mismatched type",
			method:     http.MethodPut,
			body:       `{"type":"TEXT","referenceAnswer":"reference"}`,
			querier:    &fakeQuerier{getQuestionFn: choiceQuestion(questionID)},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "rejects overlong reference answer",
			method:     http.MethodPut,
			body:       `{"type":"TEXT","referenceAnswer":"` + longReference + `"}`,
			querier:    &fakeQuerier{getQuestionFn: textQuestion(questionID)},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, "/api/questions/"+questionID.String()+"/correct-answer", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")

			newTestMux(tt.querier).ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantStatus, recorder.Code, recorder.Body.String())
			}
			if got := len(tt.querier.upsertCorrectAnswerCalls); got != tt.wantUpserts {
				t.Fatalf("upsert calls mismatch: want %d got %d", tt.wantUpserts, got)
			}
			if tt.assertBody != nil {
				var body map[string]any
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				tt.assertBody(t, body)
			}
		})
	}
}

func TestHandlerCorrectAnswerAuthorization(t *testing.T) {
	questionID := uuid.New()
	actorID := uuid.New()
	referenceAnswer := "reference"

	tests := []struct {
		name     string
		roles    []auth.Role
		method   string
		wantCode int
	}{
		{name: "student cannot get", roles: []auth.Role{auth.STUDENT}, method: http.MethodGet, wantCode: http.StatusForbidden},
		{name: "student cannot put", roles: []auth.Role{auth.STUDENT}, method: http.MethodPut, wantCode: http.StatusForbidden},
		{name: "experimenter can get", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodGet, wantCode: http.StatusOK},
		{name: "experimenter can put", roles: []auth.Role{auth.EXPERIMENTER}, method: http.MethodPut, wantCode: http.StatusOK},
		{name: "admin can get", roles: []auth.Role{auth.ADMIN}, method: http.MethodGet, wantCode: http.StatusOK},
		{name: "admin can put", roles: []auth.Role{auth.ADMIN}, method: http.MethodPut, wantCode: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{
				getQuestionFn: textQuestion(questionID),
				getCorrectAnswerFn: func(context.Context, uuid.UUID) (CorrectAnswer, error) {
					return CorrectAnswer{QuestionID: questionID, Type: "TEXT", ReferenceAnswer: pgtype.Text{String: referenceAnswer, Valid: true}, Version: 1, AnswerResultSyncStatus: "PENDING"}, nil
				},
				upsertCorrectAnswerFn: func(_ context.Context, arg UpsertCorrectAnswerParams) (CorrectAnswer, error) {
					return CorrectAnswer{QuestionID: arg.QuestionID, Type: arg.Type, ReferenceAnswer: arg.ReferenceAnswer, Version: 1, AnswerResultSyncStatus: "PENDING"}, nil
				},
			}
			mux := newAuthorizedMux(querier, actorID, tt.roles)
			body := ""
			if tt.method == http.MethodPut {
				body = `{"type":"TEXT","referenceAnswer":"` + referenceAnswer + `"}`
			}
			request := httptest.NewRequest(tt.method, "/api/questions/"+questionID.String()+"/correct-answer", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			mux.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantCode {
				t.Fatalf("status mismatch: want %d got %d, body=%s", tt.wantCode, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestValidateQuestionOptions_TableDriven(t *testing.T) {
	tests := []struct {
		name         string
		questionType string
		options      []QuestionOptionRequest
		wantErr      bool
	}{
		{
			name:         "text allows no options",
			questionType: "TEXT",
		},
		{
			name:         "choice requires options",
			questionType: "CHOICE",
			wantErr:      true,
		},
		{
			name:         "choice rejects duplicate labels",
			questionType: "CHOICE",
			options: []QuestionOptionRequest{
				{Label: "A", Content: "one"},
				{Label: "A", Content: "two"},
			},
			wantErr: true,
		},
		{
			name:         "choice accepts unique labels",
			questionType: "CHOICE",
			options: []QuestionOptionRequest{
				{Label: "A", Content: "one"},
				{Label: "B", Content: "two"},
			},
		},
		{
			name:         "unsupported type rejected",
			questionType: "BOOLEAN",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQuestionOptions(tt.questionType, tt.options)
			if tt.wantErr && !errors.Is(err, errInvalidQuestionPayload) {
				t.Fatalf("expected invalid question payload error, got %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
