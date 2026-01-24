package questions

import (
	"context"
	"errors"
	"strconv"
	"time"

	"go.uber.org/zap"
)

type Question struct {
	ID      string
	Type    string
	Content string
	Options []Option
}

type ReqQuestion struct {
	Type       string
	Content    string
	ReqOptions []ReqOption
}

type Answer struct {
	ID               string
	QuestionID       string
	SelectedOptionID int
	TextAnswer       string
	CreateAt         string
}

type ReqAnswer struct {
	SelectedOptionID int
	TextAnswer       string
}

/* type Querier interface {
	Create(ctx context.Context, arg ReqQuestion) (Question, error)
} */

type Service struct {
	logger *zap.Logger
	// we don't need queries now
	// queries Querier
}

// func NewService(logger *zap.Logger, queries Queries) *Service
func NewService(logger *zap.Logger) *Service {
	return &Service{
		logger: logger,
		// queries: queries,
	}
}

// test only, should not appear on actual product
var questionIDCounter int
var testQuestionList []Question
var answerIDCounter int
var testAnswerList []Answer

func (s *Service) CreateQuestion(ctx context.Context, arg ReqQuestion) (Question, error) {
	// handle mock ID for each ReqOption, temporary...
	var options []Option
	for i, option := range arg.ReqOptions {
		options = append(options, Option{
			ID:      strconv.Itoa(i),
			Label:   option.Label,
			Content: option.Content,
		})
	}

	question := Question{
		ID:      strconv.Itoa(questionIDCounter),
		Type:    arg.Type,
		Content: arg.Content,
		Options: options,
	}

	// temporary...
	questionIDCounter++

	testQuestionList = append(testQuestionList, question)

	// here should have some error handle after connected the database...

	return question, nil
}

func (s *Service) ListQuestion(ctx context.Context) ([]Question, error) {
	return testQuestionList, nil
}

func (s *Service) GetQuestion(ctx context.Context, ID string) (Question, error) {
	// parse UUID logic, skip now...

	for _, question := range testQuestionList {
		if question.ID == ID {
			return question, nil
		}
	}

	s.logger.Error("invalid ID")
	return Question{}, errors.New("invalid ID")
}

func (s *Service) UpdateQuestion(ctx context.Context, ID string, arg ReqQuestion) (Question, error) {
	// parse UUID logic, skip now...

	// handle mock ID for each ReqOption, temporary...
	var options []Option
	for i, option := range arg.ReqOptions {
		options = append(options, Option{
			ID:      strconv.Itoa(i),
			Label:   option.Label,
			Content: option.Content,
		})
	}

	updateQuestion := Question{
		ID:      ID,
		Type:    arg.Type,
		Content: arg.Content,
		Options: options,
	}

	for i, question := range testQuestionList {
		if question.ID == ID {
			testQuestionList[i] = updateQuestion
			return testQuestionList[i], nil
		}
	}

	s.logger.Error("invalid ID")
	return Question{}, errors.New("invalid ID")
}

func (s *Service) DelQuestion(ctx context.Context, ID string) error {
	// temporary del logic

	for i, question := range testQuestionList {
		if question.ID == ID {
			testQuestionList[i] = testQuestionList[len(testQuestionList)-1]
			testQuestionList = testQuestionList[:len(testQuestionList)-1]
			return nil
		}
	}

	s.logger.Error("invalid ID")
	return errors.New("invalid ID")
}

func (s *Service) CreateAnswer(ctx context.Context, questionID string, arg ReqAnswer) (Answer, error) {
	answer := Answer{
		ID:               strconv.Itoa(answerIDCounter),
		QuestionID:       questionID,
		SelectedOptionID: arg.SelectedOptionID,
		TextAnswer:       arg.TextAnswer,
		CreateAt:         time.Now().String(),
	}

	answerIDCounter++

	testAnswerList = append(testAnswerList, answer)

	// here should have some error handle after connected the database...

	return answer, nil
}

func (s *Service) GetAnswer(ctx context.Context, questionID string) (Answer, error) {
	// parse UUID logic, skip now...

	for _, answer := range testAnswerList {
		if answer.QuestionID == questionID {
			return answer, nil
		}
	}

	s.logger.Error("invalid ID")
	return Answer{}, errors.New("invalid ID")
}
