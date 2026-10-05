package question

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

const maxReferenceAnswerLength = 2000

type AnswerResultSyncStatus string

const (
	AnswerResultSyncStatusPending AnswerResultSyncStatus = "PENDING"
	AnswerResultSyncStatusSynced  AnswerResultSyncStatus = "SYNCED"
	AnswerResultSyncStatusFailed  AnswerResultSyncStatus = "FAILED"
)

type CorrectAnswerRequest struct {
	Type             string
	SelectedOptionID *uuid.UUID
	ReferenceAnswer  *string
}

type CorrectAnswerQuerier interface {
	GetCorrectAnswer(ctx context.Context, questionID uuid.UUID) (CorrectAnswer, error)
	UpsertCorrectAnswer(ctx context.Context, arg UpsertCorrectAnswerParams) (CorrectAnswer, error)
}

type CorrectAnswerService struct {
	logger          *zap.Logger
	querier         CorrectAnswerQuerier
	questionService *QuestionService
}

func NewCorrectAnswerService(
	querier CorrectAnswerQuerier,
	questionService *QuestionService,
	logger *zap.Logger,
) *CorrectAnswerService {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &CorrectAnswerService{
		logger:          logger,
		querier:         querier,
		questionService: questionService,
	}
}

func (s *CorrectAnswerService) Get(ctx context.Context, questionID uuid.UUID) (CorrectAnswer, error) {
	if _, err := s.questionService.Get(ctx, questionID); err != nil {
		return CorrectAnswer{}, err
	}

	answer, err := s.querier.GetCorrectAnswer(ctx, questionID)
	if err != nil {
		return CorrectAnswer{}, databaseutil.WrapDBErrorWithKeyValue(
			err,
			"correct_answers",
			"question_id",
			questionID.String(),
			s.logger,
			"get correct answer",
		)
	}
	return answer, nil
}

func (s *CorrectAnswerService) Upsert(
	ctx context.Context,
	questionID uuid.UUID,
	request CorrectAnswerRequest,
) (CorrectAnswer, error) {
	question, err := s.questionService.Get(ctx, questionID)
	if err != nil {
		return CorrectAnswer{}, err
	}

	if err := validateCorrectAnswerRequest(question.Type, request); err != nil {
		return CorrectAnswer{}, err
	}

	if request.Type == "CHOICE" {
		option, err := s.questionService.optionService.Get(ctx, *request.SelectedOptionID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return CorrectAnswer{}, fmt.Errorf("%w: selected option does not belong to the question", errInvalidCorrectAnswerPayload)
			}
			return CorrectAnswer{}, err
		}
		if option.QuestionID != questionID {
			return CorrectAnswer{}, fmt.Errorf("%w: selected option does not belong to the question", errInvalidCorrectAnswerPayload)
		}
	}

	answer, err := s.querier.UpsertCorrectAnswer(ctx, UpsertCorrectAnswerParams{
		QuestionID:       questionID,
		Type:             request.Type,
		SelectedOptionID: nullableUUID(request.SelectedOptionID),
		ReferenceAnswer:  nullableText(request.ReferenceAnswer),
	})
	if err != nil {
		if errors.Is(err, errInvalidCorrectAnswerPayload) {
			return CorrectAnswer{}, err
		}
		return CorrectAnswer{}, databaseutil.WrapDBError(err, s.logger, "upsert correct answer")
	}
	return answer, nil
}

func validateCorrectAnswerRequest(questionType string, request CorrectAnswerRequest) error {
	if request.Type != questionType {
		return fmt.Errorf("%w: type must match the question type", errInvalidCorrectAnswerPayload)
	}

	switch request.Type {
	case "CHOICE":
		if request.SelectedOptionID == nil || request.ReferenceAnswer != nil {
			return fmt.Errorf("%w: CHOICE requires only selectedOptionId", errInvalidCorrectAnswerPayload)
		}
	case "TEXT":
		if request.ReferenceAnswer == nil || request.SelectedOptionID != nil {
			return fmt.Errorf("%w: TEXT requires only referenceAnswer", errInvalidCorrectAnswerPayload)
		}
		length := utf8.RuneCountInString(*request.ReferenceAnswer)
		if length < 1 || length > maxReferenceAnswerLength {
			return fmt.Errorf(
				"%w: referenceAnswer must be between 1 and %d characters",
				errInvalidCorrectAnswerPayload,
				maxReferenceAnswerLength,
			)
		}
	default:
		return fmt.Errorf("%w: unsupported type", errInvalidCorrectAnswerPayload)
	}

	return nil
}
