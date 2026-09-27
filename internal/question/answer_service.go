package question

import (
	"context"
	"fmt"
	"math"
	"time"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

type AnswerRequest struct {
	QuestionID       uuid.UUID
	UserID           uuid.UUID
	ExperimentID     uuid.UUID
	SelectedOptionID *uuid.UUID
	TextAnswer       *string
}

type AnswerQuerier interface {
	CreateAnswer(ctx context.Context, arg CreateAnswerParams) (Answer, error)
	ListAnswersByQuestionAndExperimentPage(ctx context.Context, arg ListAnswersByQuestionAndExperimentPageParams) ([]ListAnswersByQuestionAndExperimentPageRow, error)
	CountAnswersByQuestionAndExperiment(ctx context.Context, arg CountAnswersByQuestionAndExperimentParams) (int64, error)
	IsAnswerManagementScopeReachable(ctx context.Context, arg IsAnswerManagementScopeReachableParams) (bool, error)
}

type AnswerListInput struct {
	ExperimentID uuid.UUID
	Page         int32
	PageSize     int32
}

type AnswerWithResult struct {
	ID               uuid.UUID
	QuestionID       uuid.UUID
	UserID           uuid.UUID
	ExperimentID     uuid.UUID
	SelectedOptionID *uuid.UUID
	TextAnswer       *string
	CreatedAt        time.Time
	GradingStatus    GradingStatus
	GradingMethod    *GradingMethod
	IsCorrect        *bool
	GradedAt         *time.Time
}

type AnswerPage struct {
	Items       []AnswerWithResult
	TotalPages  int32
	TotalItems  int32
	CurrentPage int32
	PageSize    int32
	HasNextPage bool
}

type AnswerService struct {
	logger          *zap.Logger
	querier         AnswerQuerier
	questionService *QuestionService
}

func NewAnswerService(querier AnswerQuerier, questionService *QuestionService, logger *zap.Logger) *AnswerService {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &AnswerService{
		logger:          logger,
		querier:         querier,
		questionService: questionService,
	}
}

func (s *AnswerService) Create(ctx context.Context, arg AnswerRequest) (Answer, error) {
	if _, err := s.validateSubmission(ctx, arg); err != nil {
		return Answer{}, err
	}

	answer, err := s.querier.CreateAnswer(ctx, CreateAnswerParams{
		QuestionID:       arg.QuestionID,
		UserID:           arg.UserID,
		ExperimentID:     arg.ExperimentID,
		SelectedOptionID: nullableUUID(arg.SelectedOptionID),
		TextAnswer:       nullableText(arg.TextAnswer),
	})
	if err != nil {
		return Answer{}, databaseutil.WrapDBError(err, s.logger, "create answer")
	}

	return answer, nil
}

func (s *AnswerService) validateSubmission(ctx context.Context, arg AnswerRequest) (Question, error) {
	question, err := s.questionService.Get(ctx, arg.QuestionID)
	if err != nil {
		return Question{}, err
	}

	if err := validateAnswerPayload(question.Type, arg); err != nil {
		return Question{}, err
	}

	if question.Type == "CHOICE" {
		belongs, err := s.optionBelongsToQuestion(ctx, arg.QuestionID, *arg.SelectedOptionID)
		if err != nil {
			return Question{}, err
		}
		if !belongs {
			return Question{}, fmt.Errorf("%w: selected option does not belong to the question", errInvalidAnswerPayload)
		}
	}

	return question, nil
}

func (s *AnswerService) ListByQuestion(
	ctx context.Context,
	questionID uuid.UUID,
	input AnswerListInput,
) (AnswerPage, error) {
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 {
		return AnswerPage{}, fmt.Errorf("%w: page must be positive and pageSize must be between 1 and 100", errInvalidAnswerPayload)
	}
	if input.ExperimentID == uuid.Nil {
		return AnswerPage{}, fmt.Errorf("%w: experimentId is required", errInvalidAnswerPayload)
	}
	offset := (int64(input.Page) - 1) * int64(input.PageSize)
	if offset > math.MaxInt32 {
		return AnswerPage{}, fmt.Errorf("%w: page offset is too large", errInvalidAnswerPayload)
	}

	reachable, err := s.querier.IsAnswerManagementScopeReachable(ctx, IsAnswerManagementScopeReachableParams{
		ExperimentID: input.ExperimentID, QuestionID: pgtype.UUID{Bytes: questionID, Valid: true},
	})
	if err != nil {
		return AnswerPage{}, databaseutil.WrapDBError(err, s.logger, "validate answer management scope")
	}
	if !reachable {
		return AnswerPage{}, databaseutil.WrapDBErrorWithKeyValue(pgx.ErrNoRows, "answer_scope", "experiment_id", input.ExperimentID.String(), s.logger, "validate answer management scope")
	}

	rows, err := s.querier.ListAnswersByQuestionAndExperimentPage(ctx, ListAnswersByQuestionAndExperimentPageParams{
		QuestionID:   questionID,
		ExperimentID: input.ExperimentID,
		PageOffset:   int32(offset),
		PageSize:     input.PageSize,
	})
	if err != nil {
		return AnswerPage{}, databaseutil.WrapDBErrorWithKeyValue(err, "answers", "question_id", questionID.String(), s.logger, "list answers")
	}

	total, err := s.querier.CountAnswersByQuestionAndExperiment(ctx, CountAnswersByQuestionAndExperimentParams{QuestionID: questionID, ExperimentID: input.ExperimentID})
	if err != nil {
		return AnswerPage{}, databaseutil.WrapDBErrorWithKeyValue(err, "answers", "question_id", questionID.String(), s.logger, "count answers")
	}
	if total > math.MaxInt32 {
		return AnswerPage{}, fmt.Errorf("answer count exceeds the supported range")
	}

	items := make([]AnswerWithResult, 0, len(rows))
	for _, row := range rows {
		items = append(items, projectAnswerRow(row))
	}

	var totalPages int32
	if total > 0 {
		totalPages = int32((total + int64(input.PageSize) - 1) / int64(input.PageSize))
	}
	return AnswerPage{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  int32(total),
		CurrentPage: input.Page,
		PageSize:    input.PageSize,
		HasNextPage: input.Page < totalPages,
	}, nil
}

func projectAnswerRow(row ListAnswersByQuestionAndExperimentPageRow) AnswerWithResult {
	answer := AnswerWithResult{
		ID:           row.ID,
		QuestionID:   row.QuestionID,
		UserID:       row.UserID,
		ExperimentID: row.ExperimentID,
	}
	if row.SelectedOptionID.Valid {
		selectedOptionID := uuid.UUID(row.SelectedOptionID.Bytes)
		answer.SelectedOptionID = &selectedOptionID
	}
	if row.TextAnswer.Valid {
		textAnswer := row.TextAnswer.String
		answer.TextAnswer = &textAnswer
	}
	if row.CreatedAt.Valid {
		answer.CreatedAt = row.CreatedAt.Time
	}

	var result *PersistedGrading
	if row.GradingStatus.Valid {
		status := GradingStatus(row.GradingStatus.String)
		result = &PersistedGrading{Status: status}
		if row.GradingMethod.Valid {
			method := GradingMethod(row.GradingMethod.String)
			result.Method = &method
		}
		if row.IsCorrect.Valid {
			isCorrect := row.IsCorrect.Bool
			result.IsCorrect = &isCorrect
		}
		if row.GradedAt.Valid {
			gradedAt := row.GradedAt.Time
			result.GradedAt = &gradedAt
		}
		if row.CorrectAnswerVersion.Valid {
			version := row.CorrectAnswerVersion.Int64
			result.CorrectAnswerVersion = &version
		}
	}
	var currentVersion *int64
	if row.CurrentCorrectAnswerVersion.Valid {
		version := row.CurrentCorrectAnswerVersion.Int64
		currentVersion = &version
	}
	projection := ProjectGrading(result, currentVersion)
	answer.GradingStatus = projection.Status
	answer.GradingMethod = projection.Method
	answer.IsCorrect = projection.IsCorrect
	answer.GradedAt = projection.GradedAt
	return answer
}

func (s *AnswerService) optionBelongsToQuestion(ctx context.Context, questionID, optionID uuid.UUID) (bool, error) {
	options, err := s.questionService.ListOptionsByQuestion(ctx, questionID)
	if err != nil {
		return false, err
	}

	for _, opt := range options {
		if opt.ID == optionID {
			return true, nil
		}
	}

	return false, nil
}

func validateAnswerPayload(questionType string, arg AnswerRequest) error {
	hasOption := arg.SelectedOptionID != nil
	hasText := arg.TextAnswer != nil

	switch questionType {
	case "CHOICE":
		if !hasOption {
			return fmt.Errorf("%w: selected option is required for CHOICE question", errInvalidAnswerPayload)
		}
		if hasText {
			return fmt.Errorf("%w: text answer is not allowed for CHOICE question", errInvalidAnswerPayload)
		}
	case "TEXT":
		if !hasText {
			return fmt.Errorf("%w: text answer is required for TEXT question", errInvalidAnswerPayload)
		}
		if hasOption {
			return fmt.Errorf("%w: selected option is not allowed for TEXT question", errInvalidAnswerPayload)
		}
	default:
		return fmt.Errorf("%w: unsupported question type", errInvalidAnswerPayload)
	}

	return nil
}

func nullableUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

func nullableText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}
