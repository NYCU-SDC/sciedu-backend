package question

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestManagementAnswerScope(t *testing.T) {
	questionID, experimentID := uuid.New(), uuid.New()
	for _, tt := range []struct {
		name, query string
		reachable   bool
		status      int
	}{
		{"required", "", true, 400},
		{"malformed", "?experimentId=bad", true, 400},
		{"unreachable", "?experimentId=" + experimentID.String(), false, 404},
		{"valid", "?experimentId=" + experimentID.String(), true, 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQuerier{scopeFn: func(_ context.Context, arg IsAnswerManagementScopeReachableParams) (bool, error) {
				require.Equal(t, experimentID, arg.ExperimentID)
				require.Equal(t, questionID, uuid.UUID(arg.QuestionID.Bytes))
				return tt.reachable, nil
			}, listAnswersFn: func(_ context.Context, arg ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error) {
				require.True(t, tt.reachable)
				require.Equal(t, experimentID, arg.ExperimentID)
				return nil, nil
			}}
			rec := httptest.NewRecorder()
			newTestMux(q).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/questions/"+questionID.String()+"/answers"+tt.query, nil))
			require.Equal(t, tt.status, rec.Code, rec.Body.String())
		})
	}
}

func TestSubmitBodyCannotChooseExperiment(t *testing.T) {
	for _, body := range []string{`{"experimentId":null}`, `{"experimentId":"00000000-0000-0000-0000-000000000001"}`, `{"ExperimentID":"anything"}`} {
		t.Run(body, func(t *testing.T) {
			var request submitAnswerRequest
			require.ErrorIs(t, json.Unmarshal([]byte(body), &request), errInvalidAnswerPayload)
		})
	}
}
