package question

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sciedu-backend/internal/auth"
)

type resultVisibilityFunc func(context.Context, uuid.UUID) (bool, error)

func (f resultVisibilityFunc) ResolveResultVisibility(ctx context.Context, id uuid.UUID) (bool, error) {
	return f(ctx, id)
}

func TestProductionAnswerResultRoute(t *testing.T) {
	owner, stranger, questionID, answerID, experimentID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, tt := range []struct {
		name                                            string
		roles                                           []auth.Role
		actor                                           uuid.UUID
		status                                          string
		show, stale, missing, mismatch, visibilityError bool
		code                                            int
		wantStatus                                      string
		wantVisible, wantCorrect                        bool
	}{
		{name: "student own", roles: []auth.Role{auth.STUDENT}, actor: owner, status: "GRADED", show: true, code: 200, wantStatus: "GRADED", wantVisible: true, wantCorrect: true},
		{name: "client cannot release score", roles: []auth.Role{auth.STUDENT}, actor: owner, status: "GRADED", code: 200, wantStatus: "GRADED"},
		{name: "other student", roles: []auth.Role{auth.STUDENT}, actor: stranger, status: "GRADED", show: true, code: 404},
		{name: "experimenter any answer", roles: []auth.Role{auth.EXPERIMENTER}, actor: stranger, status: "GRADED", code: 200, wantStatus: "GRADED", wantVisible: true, wantCorrect: true},
		{name: "admin any answer", roles: []auth.Role{auth.ADMIN}, actor: stranger, status: "GRADED", code: 200, wantStatus: "GRADED", wantVisible: true, wantCorrect: true},
		{name: "mixed role management", roles: []auth.Role{auth.STUDENT, auth.ADMIN}, actor: stranger, status: "GRADED", code: 200, wantStatus: "GRADED", wantVisible: true, wantCorrect: true},
		{name: "pending", roles: []auth.Role{auth.STUDENT}, actor: owner, status: "PENDING", show: true, code: 200, wantStatus: "PENDING", wantVisible: true},
		{name: "failed", roles: []auth.Role{auth.STUDENT}, actor: owner, status: "FAILED", show: true, code: 200, wantStatus: "FAILED", wantVisible: true},
		{name: "stale", roles: []auth.Role{auth.STUDENT}, actor: owner, status: "GRADED", show: true, stale: true, code: 200, wantStatus: "PENDING", wantVisible: true},
		{name: "missing answer", roles: []auth.Role{auth.STUDENT}, actor: owner, missing: true, code: 404},
		{name: "missing question or question mismatch", roles: []auth.Role{auth.STUDENT}, actor: owner, mismatch: true, code: 404},
		{name: "visibility failure closed", roles: []auth.Role{auth.STUDENT}, actor: owner, status: "GRADED", visibilityError: true, code: 500},
		{name: "unauthenticated", code: 401},
		{name: "no permitted role", actor: owner, code: 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			queryCalls, visibilityCalls := 0, 0
			q := &fakeQuerier{findAnswerResultFn: func(_ context.Context, arg FindAnswerResultByQuestionAndIDParams) (FindAnswerResultByQuestionAndIDRow, error) {
				queryCalls++
				require.Equal(t, answerID, arg.AnswerID)
				if tt.missing || arg.QuestionID != questionID {
					return FindAnswerResultByQuestionAndIDRow{}, pgx.ErrNoRows
				}
				version := int64(1)
				if tt.stale {
					version = 2
				}
				return FindAnswerResultByQuestionAndIDRow{AnswerID: answerID, QuestionID: questionID, UserID: owner, ExperimentID: experimentID,
					GradingStatus: pgtype.Text{String: tt.status, Valid: true}, GradingMethod: pgtype.Text{String: "DETERMINISTIC", Valid: true},
					IsCorrect: pgtype.Bool{Bool: true, Valid: tt.status == "GRADED"}, CorrectAnswerVersion: pgtype.Int8{Int64: 1, Valid: true}, CurrentCorrectAnswerVersion: pgtype.Int8{Int64: version, Valid: true}}, nil
			}}
			visibility := resultVisibilityFunc(func(_ context.Context, id uuid.UUID) (bool, error) {
				visibilityCalls++
				require.Equal(t, experimentID, id)
				if tt.visibilityError {
					return false, errors.New("visibility unavailable")
				}
				return tt.show, nil
			})
			h := NewHandler(nil, nil, nil, zap.NewNop()).WithResults(NewAnswerResultService(q, visibility, zap.NewNop()), fakeRoleQuerier{roles: tt.roles})
			mux := http.NewServeMux()
			h.RegisterRoutes(mux, nil, nil)
			pathQuestion := questionID
			if tt.mismatch {
				pathQuestion = uuid.New()
			}
			req := httptest.NewRequest(http.MethodGet, "/api/questions/"+pathQuestion.String()+"/answers/"+answerID.String()+"/result?showScore=true&resultVisible=true&experimentId="+uuid.NewString(), nil)
			if tt.actor != uuid.Nil {
				req = req.WithContext(auth.ContextWithUserID(req.Context(), tt.actor))
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			require.Equal(t, tt.code, rec.Code, rec.Body.String())
			if tt.code == 200 {
				var body map[string]any
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				require.Equal(t, tt.wantStatus, body["status"])
				require.Equal(t, tt.wantVisible, body["resultVisible"])
				_, present := body["isCorrect"]
				require.Equal(t, tt.wantCorrect, present)
			}
			if tt.code == 401 || tt.code == 403 {
				require.Zero(t, queryCalls)
			}
			if tt.actor != owner || tt.missing || tt.mismatch {
				require.Zero(t, visibilityCalls)
			}
			require.Empty(t, q.createAnswerCalls)
			require.Empty(t, q.upsertCorrectAnswerCalls)
		})
	}
}
