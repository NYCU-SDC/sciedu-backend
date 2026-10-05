package question

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

// SubmissionGradingMode is the Answer domain's view of the grading modes that
// affect submission. It deliberately does not depend on Experiment models.
type SubmissionGradingMode string

const (
	SubmissionGradingModeAutomatic SubmissionGradingMode = "AUTOMATIC"
	SubmissionGradingModeManual    SubmissionGradingMode = "MANUAL"
)

// SubmissionContext contains only Experiment-derived data needed to submit an
// Answer. A successful resolution also confirms that the question is reachable
// by the student through the current Experiment.
type SubmissionContext struct {
	ExperimentID uuid.UUID
	GradingMode  SubmissionGradingMode
}

// SubmissionContextResolver isolates Experiment-derived data from orchestration.
// The production Store adapter uses merged relationships without domain imports.
type SubmissionContextResolver interface {
	ResolveSubmissionContext(ctx context.Context, userID, questionID uuid.UUID) (SubmissionContext, error)
}

// ResultVisibilityResolver resolves the Experiment-level score visibility
// independently from submission context.
type ResultVisibilityResolver interface {
	ResolveResultVisibility(ctx context.Context, experimentID uuid.UUID) (bool, error)
}

type SubmissionGradingKind string

const (
	SubmissionGradingPending             SubmissionGradingKind = "PENDING"
	SubmissionGradingDeterministicChoice SubmissionGradingKind = "DETERMINISTIC_CHOICE"
)

// SubmissionGradingDecision tells the transaction implementation what grading
// must be persisted. Deterministic comparison remains inside that transaction
// after it reads the current correct answer.
type SubmissionGradingDecision struct {
	Kind   SubmissionGradingKind
	Status GradingStatus
	Method *GradingMethod
}

// AnswerSubmissionCommand is the validated input to atomic
// Answer/result persistence implementation.
type AnswerSubmissionCommand struct {
	Context SubmissionContext
	Answer  AnswerRequest
	Grading SubmissionGradingDecision
}

// AnswerSubmissionTransaction is the repository boundary for duplicate
// detection and atomic Answer plus grading-result persistence. Its production
// implementation revalidates the resolved scope before persisting.
type AnswerSubmissionTransaction interface {
	SubmitAnswer(ctx context.Context, command AnswerSubmissionCommand) (Answer, error)
}

type AnswerSubmissionOrchestrator struct {
	answers  *AnswerService
	contexts SubmissionContextResolver
	tx       AnswerSubmissionTransaction
}

type submissionExperimentConfig struct {
	GradingMode SubmissionGradingMode `json:"gradingMode"`
	ShowScore   bool                  `json:"showScore"`
}

func (s *Store) ResolveSubmissionContext(ctx context.Context, userID, questionID uuid.UUID) (SubmissionContext, error) {
	rows, err := s.ResolveAnswerSubmissionContexts(ctx, ResolveAnswerSubmissionContextsParams{
		UserID: userID, QuestionID: pgtype.UUID{Bytes: questionID, Valid: true},
	})
	if err != nil {
		return SubmissionContext{}, err
	}
	if len(rows) == 0 {
		return SubmissionContext{}, handlerutil.NewNotFoundError("answer_scope", "question_id", questionID.String(), "")
	}
	if len(rows) != 1 {
		return SubmissionContext{}, errors.New("multiple current experiments violate the assignment overlap invariant")
	}
	var configuration submissionExperimentConfig
	if err := json.Unmarshal(rows[0].Configuration, &configuration); err != nil {
		return SubmissionContext{}, fmt.Errorf("decode experiment configuration: %w", err)
	}
	if configuration.GradingMode != SubmissionGradingModeAutomatic && configuration.GradingMode != SubmissionGradingModeManual {
		return SubmissionContext{}, errors.New("experiment has unsupported grading mode")
	}
	return SubmissionContext{ExperimentID: rows[0].ExperimentID, GradingMode: configuration.GradingMode}, nil
}

func (s *Store) ResolveResultVisibility(ctx context.Context, experimentID uuid.UUID) (bool, error) {
	configurationJSON, err := s.AnswerExperimentConfiguration(ctx, experimentID)
	if err != nil {
		return false, err
	}
	var configuration submissionExperimentConfig
	if err := json.Unmarshal(configurationJSON, &configuration); err != nil {
		return false, fmt.Errorf("decode experiment configuration: %w", err)
	}
	return configuration.ShowScore, nil
}

// SubmitAnswer persists the Answer and its specified grading result atomically.
// Missing CorrectAnswer creates a PENDING result, not an automatically eligible job.
func (s *Store) SubmitAnswer(ctx context.Context, command AnswerSubmissionCommand) (Answer, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Answer{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.WithTx(tx)
	// Serialize submission with correct-answer writes, including the absence of
	// a correct-answer row, so a new result cannot miss version invalidation.
	if _, err := q.LockAnswerQuestion(ctx, command.Answer.QuestionID); err != nil {
		return Answer{}, err
	}
	currentQuestion, err := q.GetQuestion(ctx, command.Answer.QuestionID)
	if err != nil {
		return Answer{}, err
	}
	if err := validateAnswerPayload(currentQuestion.Type, command.Answer); err != nil {
		return Answer{}, err
	}
	if command.Answer.SelectedOptionID != nil {
		option, err := q.GetOption(ctx, *command.Answer.SelectedOptionID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && option.QuestionID != currentQuestion.ID) {
			return Answer{}, fmt.Errorf("%w: selected option does not belong to the question", errInvalidAnswerPayload)
		}
		if err != nil {
			return Answer{}, err
		}
	}
	command.Grading, err = DecideSubmissionGrading(currentQuestion.Type, command.Context.GradingMode)
	if err != nil {
		return Answer{}, err
	}
	contexts, err := q.ResolveAnswerSubmissionContexts(ctx, ResolveAnswerSubmissionContextsParams{
		UserID: command.Answer.UserID, QuestionID: nullableUUID(&command.Answer.QuestionID),
	})
	if err != nil {
		return Answer{}, err
	}
	if len(contexts) != 1 || contexts[0].ExperimentID != command.Context.ExperimentID {
		return Answer{}, errors.New("submission scope changed before persistence")
	}
	var config submissionExperimentConfig
	if err := json.Unmarshal(contexts[0].Configuration, &config); err != nil {
		return Answer{}, err
	}
	if config.GradingMode != command.Context.GradingMode {
		return Answer{}, errors.New("submission grading mode changed before persistence")
	}
	answer, err := q.CreateAnswer(ctx, CreateAnswerParams{
		QuestionID: command.Answer.QuestionID, UserID: command.Answer.UserID,
		ExperimentID:     command.Context.ExperimentID,
		SelectedOptionID: nullableUUID(command.Answer.SelectedOptionID),
		TextAnswer:       nullableText(command.Answer.TextAnswer),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == databaseutil.PGErrUniqueViolation &&
			pgErr.ConstraintName == "answers_experiment_user_question_unique" {
			return Answer{}, errDuplicateAnswer
		}
		return Answer{}, databaseutil.WrapDBError(err, zap.NewNop(), "create scoped answer")
	}

	grading, err := initialSubmissionGrading(ctx, q, command, time.Now().UTC())
	if err != nil {
		return Answer{}, err
	}
	if _, err := q.CreateAnswerResult(ctx, createAnswerResultParams(answer.ID, grading)); err != nil {
		return Answer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Answer{}, err
	}
	return answer, nil
}

type submissionCorrectAnswerReader interface {
	GetCorrectAnswer(context.Context, uuid.UUID) (CorrectAnswer, error)
}

// Pending retains the submission's method but has no evaluated version or score.
// Eligibility is determined separately by synchronization, never by status alone.
func initialSubmissionGrading(ctx context.Context, reader submissionCorrectAnswerReader, command AnswerSubmissionCommand, gradedAt time.Time) (PersistedGrading, error) {
	pending := PersistedGrading{Status: GradingStatusPending, Method: command.Grading.Method}
	if command.Grading.Kind != SubmissionGradingDeterministicChoice {
		return pending, nil
	}
	correct, err := reader.GetCorrectAnswer(ctx, command.Answer.QuestionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pending, nil
	}
	if err != nil {
		return PersistedGrading{}, err
	}
	return DeterministicChoiceGrading(*command.Answer.SelectedOptionID, uuid.UUID(correct.SelectedOptionID.Bytes), correct.Version, gradedAt), nil
}

func createAnswerResultParams(answerID uuid.UUID, grading PersistedGrading) CreateAnswerResultParams {
	params := CreateAnswerResultParams{AnswerID: answerID, Status: string(grading.Status)}
	if grading.Method != nil {
		params.Method = pgtype.Text{String: string(*grading.Method), Valid: true}
	}
	if grading.IsCorrect != nil {
		params.IsCorrect = pgtype.Bool{Bool: *grading.IsCorrect, Valid: true}
	}
	if grading.GradedAt != nil {
		params.GradedAt = pgtype.Timestamptz{Time: *grading.GradedAt, Valid: true}
	}
	if grading.CorrectAnswerVersion != nil {
		params.CorrectAnswerVersion = pgtype.Int8{Int64: *grading.CorrectAnswerVersion, Valid: true}
	}
	return params
}

func NewAnswerSubmissionOrchestrator(
	answers *AnswerService,
	contexts SubmissionContextResolver,
	tx AnswerSubmissionTransaction,
) *AnswerSubmissionOrchestrator {
	return &AnswerSubmissionOrchestrator{answers: answers, contexts: contexts, tx: tx}
}

// Submit preserves the PR #57 question, payload, and option validation before
// resolving Experiment-derived context and crossing the atomic persistence boundary.
func (s *AnswerSubmissionOrchestrator) Submit(ctx context.Context, request AnswerRequest) (Answer, error) {
	question, err := s.answers.validateSubmission(ctx, request)
	if err != nil {
		return Answer{}, err
	}

	submissionContext, err := s.contexts.ResolveSubmissionContext(ctx, request.UserID, request.QuestionID)
	if err != nil {
		return Answer{}, err
	}

	grading, err := DecideSubmissionGrading(question.Type, submissionContext.GradingMode)
	if err != nil {
		return Answer{}, err
	}

	return s.tx.SubmitAnswer(ctx, AnswerSubmissionCommand{
		Context: submissionContext,
		Answer:  request,
		Grading: grading,
	})
}

// DecideSubmissionGrading keeps the confirmed AUTOMATIC CHOICE deterministic
// behavior and TEXT pending behavior. Recording MANUAL as the method for pending
// MANUAL submissions, and omitting the method for pending AUTOMATIC TEXT, are
// reasonable pre-integration assumptions that still require maintainer confirmation.
func DecideSubmissionGrading(questionType string, mode SubmissionGradingMode) (SubmissionGradingDecision, error) {
	switch mode {
	case SubmissionGradingModeAutomatic:
		if questionType == "CHOICE" {
			method := GradingMethodDeterministic
			return SubmissionGradingDecision{
				Kind:   SubmissionGradingDeterministicChoice,
				Status: GradingStatusGraded,
				Method: &method,
			}, nil
		}
		if questionType == "TEXT" {
			return SubmissionGradingDecision{Kind: SubmissionGradingPending, Status: GradingStatusPending}, nil
		}
	case SubmissionGradingModeManual:
		if questionType == "CHOICE" || questionType == "TEXT" {
			method := GradingMethodManual
			return SubmissionGradingDecision{
				Kind:   SubmissionGradingPending,
				Status: GradingStatusPending,
				Method: &method,
			}, nil
		}
	}

	return SubmissionGradingDecision{}, fmt.Errorf(
		"%w: unsupported question type or grading mode",
		errInvalidAnswerPayload,
	)
}
