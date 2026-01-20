package questions

import (
	"context"
	"sciedu-backend/internal/database"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Querier interface {
	CreateQuestion(ctx context.Context, arg CreateQuestionParams) (Question, error)
	CreateCorrespondOption(ctx context.Context, arg CreateCorrespondOptionParams) (Option, error)
	ListQuestion(ctx context.Context) ([]Question, error)
	GetQuestion(ctx context.Context, id uuid.UUID) (Question, error)
	ListCorrespondOption(ctx context.Context, questionID uuid.UUID) ([]Option, error)
	UpdateQuestion(ctx context.Context, arg UpdateQuestionParams) (Question, error)
	UpdateCorrespondingOption(ctx context.Context, arg UpdateCorrespondingOptionParams) (Option, error)
	DeleteQuestion(ctx context.Context, id uuid.UUID) error
	DeleteCorrespondingOption(ctx context.Context, questionID uuid.UUID) error

	SubmitAnswer(ctx context.Context, arg SubmitAnswerParams) (Answer, error)
	ListAnswer(ctx context.Context, questionID uuid.UUID) ([]Answer, error)
}

type Service struct {
	logger  *zap.Logger
	queries Querier
}

func NewService(logger *zap.Logger, db DBTX) *Service {
	return &Service{
		logger:  logger,
		queries: New(db),
	}
}

// test only, should not appear on actual product
var questionIDCounter int
var testQuestionList []Question
var answerIDCounter int
var testAnswerList []Answer

func (s *Service) CreateQuestion(ctx context.Context, arg QuestionRequest) (BoundQuestion, error) {
	question, err := s.queries.CreateQuestion(ctx, CreateQuestionParams{
		Type:    arg.Type,
		Content: arg.Content,
	})
	if err != nil {
		err = database.WrapDBError(err, s.logger, "failed to create question")
		return BoundQuestion{}, err
	}

	var options []Option
	for _, optionReq := range arg.Options {
		option, err := s.queries.CreateCorrespondOption(ctx, CreateCorrespondOptionParams{
			QuestionID: question.ID,
			Label:      optionReq.Label,
			Content:    optionReq.Content,
		})
		if err != nil {
			err = database.WrapDBError(err, s.logger, "failed to create corresponding options")
			return BoundQuestion{}, err
		}
		options = append(options, option)
	}

	res := BoundQuestion{
		Question: question,
		Options:  options,
	}

	return res, nil
}

func (s *Service) ListQuestion(ctx context.Context) ([]BoundQuestion, error) {
	questions, err := s.queries.ListQuestion(ctx)
	if err != nil {
		err = database.WrapDBError(err, s.logger, "failed to get questions")
		return []BoundQuestion{}, err
	}

	var res []BoundQuestion
	for _, question := range questions {
		options, err := s.queries.ListCorrespondOption(ctx, question.ID)
		if err != nil {
			err = database.WrapDBError(err, s.logger, "failed to get corresponding options")
			return []BoundQuestion{}, err
		}
		res = append(res, BoundQuestion{
			Question: question,
			Options:  options,
		})
	}

	return res, nil
}

func (s *Service) GetQuestion(ctx context.Context, ID uuid.UUID) (BoundQuestion, error) {
	question, err := s.queries.GetQuestion(ctx, ID)
	if err != nil {
		err = database.WrapDBError(err, s.logger, "failed to get question")
		return BoundQuestion{}, err
	}

	options, err := s.queries.ListCorrespondOption(ctx, question.ID)
	if err != nil {
		err = database.WrapDBError(err, s.logger, "failed to get corresponding options")
		return BoundQuestion{}, err
	}

	return BoundQuestion{
		Question: question,
		Options:  options,
	}, nil
}

func (s *Service) UpdateQuestion(ctx context.Context, ID uuid.UUID, arg QuestionRequest) (BoundQuestion, error) {
	question, err := s.queries.UpdateQuestion(ctx, UpdateQuestionParams{
		ID:      ID,
		Type:    arg.Type,
		Content: arg.Content,
	})
	if err != nil {
		err = database.WrapDBError(err, s.logger, "failed to update question")
		return BoundQuestion{}, err
	}

	// option process, a little bit complex
	typeCheckOp, err := s.queries.ListCorrespondOption(ctx, question.ID)
	if err != nil {
		err = database.WrapDBError(err, s.logger, "failed to get corresponding options when updating question")
		return BoundQuestion{}, err
	}

	var options []Option
	if len(typeCheckOp) == 0 { // create options for it
		// create options from arg
		for _, optionReq := range arg.Options {
			option, err := s.queries.CreateCorrespondOption(ctx, CreateCorrespondOptionParams{
				QuestionID: question.ID,
				Label:      optionReq.Label,
				Content:    optionReq.Content,
			})
			if err != nil {
				err = database.WrapDBError(err, s.logger, "failed to create corresponding options")
				return BoundQuestion{}, err
			}
			options = append(options, option)
		}
	} else if len(arg.Options) == 0 { // delete options for it
		err = s.queries.DeleteCorrespondingOption(ctx, question.ID)
		if err != nil {
			err = database.WrapDBError(err, s.logger, "failed to delete corresponding options when updating question")
			return BoundQuestion{}, err
		}
	} else { // type remains unchanged
		for i := 0; i < len(arg.Options); i++ {
			option, err := s.queries.UpdateCorrespondingOption(ctx, UpdateCorrespondingOptionParams{
				QuestionID: question.ID,
				Label:      arg.Options[i].Label,
				Content:    arg.Options[i].Content,
			})
			if err != nil {
				err = database.WrapDBError(err, s.logger, "failed to update corresponding options")
				return BoundQuestion{}, err
			}
			options = append(options, option)
		}
	}

	return BoundQuestion{
		Question: question,
		Options:  options,
	}, nil
}

func (s *Service) DelQuestion(ctx context.Context, ID uuid.UUID) error {
	err := s.queries.DeleteQuestion(ctx, ID)
	return err
}

/* Answer */

func (s *Service) SubmitAnswer(ctx context.Context, arg SubmitAnswerParams) (Answer, error) {
	answer, err := s.queries.SubmitAnswer(ctx, SubmitAnswerParams{
		QuestionID:       arg.QuestionID,
		SelectedOptionID: arg.SelectedOptionID,
		TextAnswer:       arg.TextAnswer,
	})
	if err != nil {
		err = database.WrapDBError(err, s.logger, "failed to submit answer")
		return Answer{}, err
	}

	return answer, nil
}

func (s *Service) ListAnswer(ctx context.Context, questionID uuid.UUID) ([]Answer, error) {
	answers, err := s.queries.ListAnswer(ctx, questionID)
	if err != nil {
		err = database.WrapDBError(err, s.logger, "failed to list answers")
		return []Answer{}, err
	}

	return answers, nil
}
