package questions

import (
	"context"
	"errors"
	"strconv"

	"go.uber.org/zap"
)

// test only, should not appear on actual product
var testQuestionList []Question

type Question struct {
	ID      string
	Type    string
	Content string
	Options []Option
}

type ReqParam struct {
	Type       string
	Content    string
	ReqOptions []ReqOption
}

type Querier interface {
	Create(ctx context.Context, arg ReqParam) (Question, error)
}

type Service struct {
	logger *zap.Logger
	// we don't need queries now
	// queries Querier
}

// func NewService(logger *zap.Logger, queries Querier) *Service
func NewService(logger *zap.Logger) *Service {
	return &Service{
		logger: logger,
		// queries: queries,
	}
}

var IDCounter int

func (s *Service) Create(ctx context.Context, arg ReqParam) (Question, error) {
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
		ID:      strconv.Itoa(IDCounter),
		Type:    arg.Type,
		Content: arg.Content,
		Options: options,
	}

	// temporary...
	IDCounter++

	testQuestionList = append(testQuestionList, question)

	// here should have some error handle after connected the database...

	return question, nil
}

func (s *Service) List(ctx context.Context) ([]Question, error) {
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

func (s *Service) UpdateQuestion(ctx context.Context, ID string, arg ReqParam) (Question, error) {
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
