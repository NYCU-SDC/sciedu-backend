package question

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeSubmissionContextResolver struct {
	resolveFn func(ctx context.Context, userID, questionID uuid.UUID) (SubmissionContext, error)
	calls     int
}

func (f *fakeSubmissionContextResolver) ResolveSubmissionContext(
	ctx context.Context,
	userID, questionID uuid.UUID,
) (SubmissionContext, error) {
	f.calls++
	return f.resolveFn(ctx, userID, questionID)
}

type fakeAnswerSubmissionTransaction struct {
	submitFn func(ctx context.Context, command AnswerSubmissionCommand) (Answer, error)
	calls    int
}

func (f *fakeAnswerSubmissionTransaction) SubmitAnswer(
	ctx context.Context,
	command AnswerSubmissionCommand,
) (Answer, error) {
	f.calls++
	return f.submitFn(ctx, command)
}

func TestAnswerSubmissionOrchestrator_TableDriven(t *testing.T) {
	resolverError := errors.New("resolve submission context")
	transactionError := errors.New("persist answer transaction")
	questionID := uuid.New()
	userID := uuid.New()
	experimentID := uuid.New()
	correctOptionID := uuid.New()
	textAnswer := "answer"

	tests := []struct {
		name              string
		questionType      string
		request           AnswerRequest
		mode              SubmissionGradingMode
		resolverErr       error
		transactionErr    error
		options           []Option
		wantErr           error
		wantResolverCalls int
		wantTxCalls       int
		wantKind          SubmissionGradingKind
		wantStatus        GradingStatus
		wantMethod        *GradingMethod
	}{
		{
			name:         "automatic choice crosses transaction boundary with deterministic plan",
			questionType: "CHOICE",
			request: AnswerRequest{QuestionID: questionID, UserID: userID,
				SelectedOptionID: &correctOptionID},
			mode: SubmissionGradingModeAutomatic, options: []Option{{ID: correctOptionID, QuestionID: questionID}},
			wantResolverCalls: 1, wantTxCalls: 1,
			wantKind: SubmissionGradingDeterministicChoice, wantStatus: GradingStatusGraded,
			wantMethod: gradingMethodPointer(GradingMethodDeterministic),
		},
		{
			name:         "manual choice remains pending",
			questionType: "CHOICE",
			request: AnswerRequest{QuestionID: questionID, UserID: userID,
				SelectedOptionID: &correctOptionID},
			mode: SubmissionGradingModeManual, options: []Option{{ID: correctOptionID, QuestionID: questionID}},
			wantResolverCalls: 1, wantTxCalls: 1,
			wantKind: SubmissionGradingPending, wantStatus: GradingStatusPending,
			wantMethod: gradingMethodPointer(GradingMethodManual),
		},
		{
			name:         "automatic text remains pending without LLM method",
			questionType: "TEXT",
			request: AnswerRequest{QuestionID: questionID, UserID: userID,
				TextAnswer: &textAnswer},
			mode:              SubmissionGradingModeAutomatic,
			wantResolverCalls: 1, wantTxCalls: 1,
			wantKind: SubmissionGradingPending, wantStatus: GradingStatusPending,
		},
		{
			name:         "manual text remains pending for manual grading",
			questionType: "TEXT",
			request: AnswerRequest{QuestionID: questionID, UserID: userID,
				TextAnswer: &textAnswer},
			mode:              SubmissionGradingModeManual,
			wantResolverCalls: 1, wantTxCalls: 1,
			wantKind: SubmissionGradingPending, wantStatus: GradingStatusPending,
			wantMethod: gradingMethodPointer(GradingMethodManual),
		},
		{
			name:         "existing payload validation runs before context resolution",
			questionType: "CHOICE",
			request: AnswerRequest{QuestionID: questionID, UserID: userID,
				TextAnswer: &textAnswer},
			mode:    SubmissionGradingModeAutomatic,
			wantErr: errInvalidAnswerPayload,
		},
		{
			name:         "existing option ownership validation runs before context resolution",
			questionType: "CHOICE",
			request: AnswerRequest{QuestionID: questionID, UserID: userID,
				SelectedOptionID: &correctOptionID},
			mode:    SubmissionGradingModeAutomatic,
			wantErr: errInvalidAnswerPayload,
		},
		{
			name:         "resolver failure prevents persistence",
			questionType: "TEXT",
			request: AnswerRequest{QuestionID: questionID, UserID: userID,
				TextAnswer: &textAnswer},
			mode: SubmissionGradingModeAutomatic, resolverErr: resolverError,
			wantErr: resolverError, wantResolverCalls: 1,
		},
		{
			name:         "transaction failure is propagated",
			questionType: "TEXT",
			request: AnswerRequest{QuestionID: questionID, UserID: userID,
				TextAnswer: &textAnswer},
			mode: SubmissionGradingModeAutomatic, transactionErr: transactionError,
			wantErr: transactionError, wantResolverCalls: 1, wantTxCalls: 1,
			wantKind: SubmissionGradingPending, wantStatus: GradingStatusPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeQuerier{
				getQuestionFn: func(context.Context, uuid.UUID) (Question, error) {
					return Question{ID: questionID, Type: tt.questionType}, nil
				},
				listOptionsByQuestionFn: func(context.Context, uuid.UUID) ([]Option, error) {
					return tt.options, nil
				},
			}
			optionService := NewOptionService(querier, zap.NewNop())
			questionService := NewQuestionService(querier, optionService, zap.NewNop())
			answerService := NewAnswerService(querier, questionService, zap.NewNop())
			resolver := &fakeSubmissionContextResolver{resolveFn: func(_ context.Context, gotUserID, gotQuestionID uuid.UUID) (SubmissionContext, error) {
				assert.Equal(t, userID, gotUserID)
				assert.Equal(t, questionID, gotQuestionID)
				return SubmissionContext{ExperimentID: experimentID, GradingMode: tt.mode}, tt.resolverErr
			}}
			tx := &fakeAnswerSubmissionTransaction{submitFn: func(_ context.Context, command AnswerSubmissionCommand) (Answer, error) {
				assert.Equal(t, experimentID, command.Context.ExperimentID)
				assert.Equal(t, tt.request, command.Answer)
				assert.Equal(t, tt.wantKind, command.Grading.Kind)
				assert.Equal(t, tt.wantStatus, command.Grading.Status)
				assert.Equal(t, tt.wantMethod, command.Grading.Method)
				return Answer{ID: uuid.New(), QuestionID: questionID, UserID: userID}, tt.transactionErr
			}}

			answer, err := NewAnswerSubmissionOrchestrator(answerService, resolver, tx).Submit(t.Context(), tt.request)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, tt.wantResolverCalls, resolver.calls)
				assert.Equal(t, tt.wantTxCalls, tx.calls)
				return
			}
			require.NoError(t, err)
			assert.NotEqual(t, uuid.Nil, answer.ID)
			assert.Equal(t, tt.wantResolverCalls, resolver.calls)
			assert.Equal(t, tt.wantTxCalls, tx.calls)
		})
	}
}

func TestDecideSubmissionGradingRejectsUnknownInputs(t *testing.T) {
	tests := []struct {
		name         string
		questionType string
		mode         SubmissionGradingMode
	}{
		{name: "unknown question type", questionType: "VIDEO", mode: SubmissionGradingModeAutomatic},
		{name: "unknown grading mode", questionType: "CHOICE", mode: "HYBRID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecideSubmissionGrading(tt.questionType, tt.mode)
			require.ErrorIs(t, err, errInvalidAnswerPayload)
		})
	}
}

func gradingMethodPointer(method GradingMethod) *GradingMethod { return &method }
