package question

import (
	"context"
	"fmt"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type QuestionRequest struct {
	Type    string
	Content string
}

type QuestionQuerier interface {
	ListQuestion(ctx context.Context) ([]Question, error)
	GetQuestion(ctx context.Context, id uuid.UUID) (Question, error)
	CreateQuestion(ctx context.Context, arg CreateQuestionParams) (Question, error)
	UpdateQuestion(ctx context.Context, arg UpdateQuestionParams) (Question, error)
	DeleteQuestion(ctx context.Context, id uuid.UUID) error
}

type QuestionService struct {
	logger        *zap.Logger
	querier       QuestionQuerier
	optionService *OptionService
}

func NewQuestionService(querier QuestionQuerier, optionService *OptionService, logger *zap.Logger) *QuestionService {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &QuestionService{
		logger:        logger,
		querier:       querier,
		optionService: optionService,
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

func (s *QuestionService) BuildQuestionResponse(ctx context.Context, q Question) (questionResponse, error) {
	resp := questionResponse{
		ID:      q.ID,
		Type:    q.Type,
		Content: q.Content,
	}

	if q.Type != "CHOICE" {
		return resp, nil
	}

	opts, err := s.optionService.ListByQuestion(ctx, q.ID)
	if err != nil {
		return questionResponse{}, err
	}

	resp.Options = make([]optionResponse, 0, len(opts))
	for _, opt := range opts {
		resp.Options = append(resp.Options, optionResponse{
			ID:      opt.ID,
			Label:   opt.Label,
			Content: opt.Content,
		})
	}

	return resp, nil
}

func (s *QuestionService) SyncQuestionOptions(ctx context.Context, questionID uuid.UUID, questionType string, options []createUpdateOptionRequest, replace bool) error {
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

	if len(options) == 0 {
		return fmt.Errorf("%w: options are required for CHOICE question", errInvalidQuestionPayload)
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
