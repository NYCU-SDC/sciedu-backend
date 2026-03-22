package question

import (
	"context"

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
	logger  *zap.Logger
	querier QuestionQuerier
}

func NewQuestionService(querier QuestionQuerier, logger *zap.Logger) *QuestionService {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &QuestionService{
		logger:  logger,
		querier: querier,
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
