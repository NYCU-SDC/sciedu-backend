package question

import (
	"context"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type OptionRequest struct {
	QuestionID uuid.UUID
	Label      string
	Content    string
}

type OptionQuerier interface {
	GetOption(ctx context.Context, id uuid.UUID) (Option, error)
	ListOptionsByQuestion(ctx context.Context, questionID uuid.UUID) ([]Option, error)
	CreateOption(ctx context.Context, arg CreateOptionParams) (Option, error)
	UpdateOption(ctx context.Context, arg UpdateOptionParams) (Option, error)
	DeleteOption(ctx context.Context, id uuid.UUID) error
}

type OptionService struct {
	logger  *zap.Logger
	querier OptionQuerier
}

func NewOptionService(querier OptionQuerier, logger *zap.Logger) *OptionService {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &OptionService{
		logger:  logger,
		querier: querier,
	}
}

func (s *OptionService) Get(ctx context.Context, id uuid.UUID) (Option, error) {
	option, err := s.querier.GetOption(ctx, id)
	if err != nil {
		return Option{}, databaseutil.WrapDBErrorWithKeyValue(err, "options", "id", id.String(), s.logger, "get option")
	}
	return option, nil
}

func (s *OptionService) ListByQuestion(ctx context.Context, questionID uuid.UUID) ([]Option, error) {
	options, err := s.querier.ListOptionsByQuestion(ctx, questionID)
	if err != nil {
		return nil, databaseutil.WrapDBErrorWithKeyValue(err, "options", "question_id", questionID.String(), s.logger, "list options")
	}
	return options, nil
}

func (s *OptionService) Create(ctx context.Context, arg OptionRequest) (Option, error) {
	option, err := s.querier.CreateOption(ctx, CreateOptionParams(arg))
	if err != nil {
		return Option{}, databaseutil.WrapDBError(err, s.logger, "create option")
	}
	return option, nil
}

func (s *OptionService) Update(ctx context.Context, id uuid.UUID, arg OptionRequest) (Option, error) {
	option, err := s.querier.UpdateOption(ctx, UpdateOptionParams{
		ID:      id,
		Label:   arg.Label,
		Content: arg.Content,
	})
	if err != nil {
		return Option{}, databaseutil.WrapDBErrorWithKeyValue(err, "options", "id", id.String(), s.logger, "update option")
	}
	return option, nil
}

func (s *OptionService) Delete(ctx context.Context, id uuid.UUID) error {
	return databaseutil.WrapDBErrorWithKeyValue(s.querier.DeleteOption(ctx, id), "options", "id", id.String(), s.logger, "delete option")
}
