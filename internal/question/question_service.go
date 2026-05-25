package question

import (
	"context"
	"errors"
	"fmt"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var errQuestionTransactionUnsupported = errors.New("question transaction unsupported")

type QuestionRequest struct {
	Type    string
	Content string
}

type QuestionOptionRequest struct {
	Label   string
	Content string
}

type QuestionQuerier interface {
	ListQuestion(ctx context.Context) ([]Question, error)
	GetQuestion(ctx context.Context, id uuid.UUID) (Question, error)
	CreateQuestion(ctx context.Context, arg CreateQuestionParams) (Question, error)
	UpdateQuestion(ctx context.Context, arg UpdateQuestionParams) (Question, error)
	DeleteQuestion(ctx context.Context, id uuid.UUID) error
}

type QuestionTransactor interface {
	WithinTx(ctx context.Context, fn func(QuestionQuerier, OptionQuerier) error) error
}

type QuestionService struct {
	logger        *zap.Logger
	querier       QuestionQuerier
	optionService *OptionService
	transactor    QuestionTransactor
}

func NewQuestionService(querier QuestionQuerier, optionService *OptionService, logger *zap.Logger) *QuestionService {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &QuestionService{
		logger:        logger,
		querier:       querier,
		optionService: optionService,
		transactor:    transactorFromQuerier(querier),
	}
}

func (s *QuestionService) List(ctx context.Context) ([]Question, error) {
	questions, err := s.querier.ListQuestion(ctx)
	if err != nil {
		return nil, databaseutil.WrapDBError(err, s.logger, "list questions")
	}
	return questions, nil
}

func (s *QuestionService) Get(ctx context.Context, id uuid.UUID) (Question, error) {
	question, err := s.querier.GetQuestion(ctx, id)
	if err != nil {
		return Question{}, databaseutil.WrapDBErrorWithKeyValue(err, "questions", "id", id.String(), s.logger, "get question")
	}
	return question, nil
}

func (s *QuestionService) Create(ctx context.Context, arg QuestionRequest) (Question, error) {
	question, err := s.querier.CreateQuestion(ctx, CreateQuestionParams(arg))
	if err != nil {
		return Question{}, databaseutil.WrapDBError(err, s.logger, "create question")
	}
	return question, nil
}

func (s *QuestionService) Update(ctx context.Context, id uuid.UUID, arg QuestionRequest) (Question, error) {
	question, err := s.querier.UpdateQuestion(ctx, UpdateQuestionParams{
		ID:      id,
		Type:    arg.Type,
		Content: arg.Content,
	})
	if err != nil {
		return Question{}, databaseutil.WrapDBErrorWithKeyValue(err, "questions", "id", id.String(), s.logger, "update question")
	}
	return question, nil
}

func (s *QuestionService) Delete(ctx context.Context, id uuid.UUID) error {
	return databaseutil.WrapDBErrorWithKeyValue(s.querier.DeleteQuestion(ctx, id), "questions", "id", id.String(), s.logger, "delete question")
}

func (s *QuestionService) CreateWithOptions(ctx context.Context, arg QuestionRequest, options []QuestionOptionRequest) (Question, error) {
	if err := validateQuestionOptions(arg.Type, options); err != nil {
		return Question{}, err
	}

	var question Question
	err := s.withinTx(ctx, func(questionQuerier QuestionQuerier, optionQuerier OptionQuerier) error {
		txQuestionService := NewQuestionService(questionQuerier, NewOptionService(optionQuerier, s.logger), s.logger)
		created, err := txQuestionService.Create(ctx, arg)
		if err != nil {
			return err
		}

		if err := txQuestionService.SyncQuestionOptions(ctx, created.ID, arg.Type, options, false); err != nil {
			return err
		}

		question = created
		return nil
	})
	if err != nil {
		return Question{}, err
	}

	return question, nil
}

func (s *QuestionService) UpdateWithOptions(ctx context.Context, id uuid.UUID, arg QuestionRequest, options []QuestionOptionRequest) (Question, error) {
	if err := validateQuestionOptions(arg.Type, options); err != nil {
		return Question{}, err
	}

	var question Question
	err := s.withinTx(ctx, func(questionQuerier QuestionQuerier, optionQuerier OptionQuerier) error {
		txQuestionService := NewQuestionService(questionQuerier, NewOptionService(optionQuerier, s.logger), s.logger)
		if _, err := txQuestionService.Get(ctx, id); err != nil {
			return err
		}

		updated, err := txQuestionService.Update(ctx, id, arg)
		if err != nil {
			return err
		}

		if err := txQuestionService.SyncQuestionOptions(ctx, id, arg.Type, options, true); err != nil {
			return err
		}

		question = updated
		return nil
	})
	if err != nil {
		return Question{}, err
	}

	return question, nil
}

func (s *QuestionService) SyncQuestionOptions(ctx context.Context, questionID uuid.UUID, questionType string, options []QuestionOptionRequest, replace bool) error {
	if err := validateQuestionOptions(questionType, options); err != nil {
		return err
	}

	if replace || questionType == "TEXT" {
		existing, err := s.optionService.ListByQuestion(ctx, questionID)
		if err != nil {
			return err
		}
		for _, opt := range existing {
			if err := s.optionService.Delete(ctx, opt.ID); err != nil {
				return err
			}
		}
	}

	if questionType == "TEXT" {
		return nil
	}

	for _, opt := range options {
		if _, err := s.optionService.Create(ctx, OptionRequest{
			QuestionID: questionID,
			Label:      opt.Label,
			Content:    opt.Content,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *QuestionService) ListOptionsByQuestion(ctx context.Context, questionID uuid.UUID) ([]Option, error) {
	return s.optionService.ListByQuestion(ctx, questionID)
}

func (s *QuestionService) withinTx(ctx context.Context, fn func(QuestionQuerier, OptionQuerier) error) error {
	if s.transactor == nil {
		return errQuestionTransactionUnsupported
	}
	return s.transactor.WithinTx(ctx, fn)
}

func validateQuestionOptions(questionType string, options []QuestionOptionRequest) error {
	switch questionType {
	case "TEXT":
		return nil
	case "CHOICE":
	default:
		return fmt.Errorf("%w: unsupported question type", errInvalidQuestionPayload)
	}

	if len(options) == 0 {
		return fmt.Errorf("%w: options are required for CHOICE question", errInvalidQuestionPayload)
	}

	seen := make(map[string]struct{}, len(options))
	for _, opt := range options {
		if _, ok := seen[opt.Label]; ok {
			return fmt.Errorf("%w: option labels must be unique", errInvalidQuestionPayload)
		}
		seen[opt.Label] = struct{}{}
	}

	return nil
}

func transactorFromQuerier(querier QuestionQuerier) QuestionTransactor {
	transactor, ok := querier.(QuestionTransactor)
	if !ok {
		return nil
	}
	return transactor
}
