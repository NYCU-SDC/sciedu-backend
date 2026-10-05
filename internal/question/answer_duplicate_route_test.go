package question

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"sciedu-backend/internal/auth"
)

type duplicateAnswerTx struct {
	lockedValidationTx
	experimentID uuid.UUID
	insertErr    error
	inserted     bool
}

func (tx *duplicateAnswerTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if sql == createAnswer {
		tx.inserted = true
		return validationRow(func(...any) error { return tx.insertErr })
	}
	return tx.lockedValidationTx.QueryRow(ctx, sql, args...)
}

func (tx *duplicateAnswerTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	require.Equal(tx.t, resolveAnswerSubmissionContexts, sql)
	return &duplicateScopeRows{experimentID: tx.experimentID}, nil
}

type duplicateScopeRows struct {
	pgx.Rows
	experimentID uuid.UUID
	read         bool
}

func (r *duplicateScopeRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}
func (r *duplicateScopeRows) Close()     {}
func (r *duplicateScopeRows) Err() error { return nil }
func (r *duplicateScopeRows) Scan(dest ...any) error {
	*dest[0].(*uuid.UUID) = r.experimentID
	*dest[1].(*[]byte) = []byte(`{"gradingMode":"AUTOMATIC"}`)
	return nil
}

type duplicateAnswerDB struct {
	DBTX
	tx *duplicateAnswerTx
}

func (d duplicateAnswerDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }

func TestSubmitAnswerRouteDuplicateConflict(t *testing.T) {
	for _, tt := range []struct {
		name, code, constraint string
		want                   int
	}{
		{"scoped duplicate", "23505", "answers_experiment_user_question_unique", http.StatusConflict},
		{"unrelated unique violation", "23505", "answers_pkey", http.StatusInternalServerError},
		{"different database error", "23503", "answers_experiment_user_question_unique", http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			questionID, experimentID := uuid.New(), uuid.New()
			tx := &duplicateAnswerTx{
				lockedValidationTx: lockedValidationTx{t: t, questionID: questionID, currentType: "TEXT"},
				experimentID:       experimentID,
				insertErr:          fmt.Errorf("insert: %w", &pgconn.PgError{Code: tt.code, ConstraintName: tt.constraint}),
			}
			store := NewStore(duplicateAnswerDB{tx: tx})
			q := &fakeQuerier{getQuestionFn: textQuestion(questionID)}
			logger := zap.NewNop()
			questions := NewQuestionService(q, NewOptionService(q, logger), logger)
			answers := NewAnswerService(q, questions, logger)
			resolver := &fakeSubmissionContextResolver{resolveFn: func(context.Context, uuid.UUID, uuid.UUID) (SubmissionContext, error) {
				return SubmissionContext{ExperimentID: experimentID, GradingMode: SubmissionGradingModeAutomatic}, nil
			}}
			h := NewHandler(questions, answers, nil, logger).WithSubmission(NewAnswerSubmissionOrchestrator(answers, resolver, store))
			mux := http.NewServeMux()
			h.RegisterRoutes(mux, nil, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/questions/"+questionID.String()+"/answers", strings.NewReader(`{"textAnswer":"answer"}`))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(auth.ContextWithUserID(req.Context(), uuid.New()))
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			require.Equal(t, tt.want, response.Code, response.Body.String())
			require.Contains(t, response.Header().Get("Content-Type"), "application/problem+json")
			require.True(t, tx.inserted)
			require.True(t, tx.rolledBack)
			require.False(t, tx.committed)
		})
	}
}
