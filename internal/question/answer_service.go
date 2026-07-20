package question

import (
	"context"
	"fmt"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

type AnswerRequest struct {
	QuestionID       uuid.UUID
	UserID           uuid.UUID
	SelectedOptionID *uuid.UUID
	TextAnswer       *string
}

type AnswerQuerier interface {
	CreateAnswer(ctx context.Context, arg CreateAnswerParams) (Answer, error)
	ListAnswersByQuestionForUser(ctx context.Context, arg ListAnswersByQuestionForUserParams) ([]Answer, error)
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
	question, err := s.questionService.Get(ctx, arg.QuestionID)
	if err != nil {
		return Answer{}, err
	}

	if err := validateAnswerPayload(question.Type, arg); err != nil {
		return Answer{}, err
	}

	if question.Type == "CHOICE" {
		belongs, err := s.optionBelongsToQuestion(ctx, arg.QuestionID, *arg.SelectedOptionID)
		if err != nil {
			return Answer{}, err
		}
		if !belongs {
			return Answer{}, fmt.Errorf("%w: selected option does not belong to the question", errInvalidAnswerPayload)
		}
	}

	answer, err := s.querier.CreateAnswer(ctx, CreateAnswerParams{
		QuestionID:       arg.QuestionID,
		UserID:           arg.UserID,
		SelectedOptionID: nullableUUID(arg.SelectedOptionID),
		TextAnswer:       nullableText(arg.TextAnswer),
	})
	if err != nil {
		return Answer{}, databaseutil.WrapDBError(err, s.logger, "create answer")
	}

	return answer, nil
}

func (s *AnswerService) ListByQuestionForUser(ctx context.Context, questionID, userID uuid.UUID) ([]Answer, error) {
	answers, err := s.querier.ListAnswersByQuestionForUser(ctx, ListAnswersByQuestionForUserParams{
		QuestionID: questionID,
		UserID:     userID,
	})
	if err != nil {
		return nil, databaseutil.WrapDBErrorWithKeyValue(err, "answers", "question_id", questionID.String(), s.logger, "list answers")
	}

	return answers, nil
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
