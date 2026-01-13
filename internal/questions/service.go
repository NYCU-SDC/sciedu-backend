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
		ID:      strconv.Itoa(len(testQuestionList)),
		Type:    arg.Type,
		Content: arg.Content,
		Options: options,
	}

	testQuestionList = append(testQuestionList, question)

	// here should have some error handle after connected the database...

	return question, nil
}

func (s *Service) List(ctx context.Context) ([]Question, error) {
	return testQuestionList, nil
}

func (s *Service) GetQuestion(ctx context.Context, ID string) (Question, error) {
	// parse UUID logic, skip now...

	// temporary string convert logic
	questionID, err := strconv.Atoi(ID)
	if err != nil {
		s.logger.Error("failed to convert ID from string to int", zap.Error(err))
		return Question{}, err
	}

	if questionID >= len(testQuestionList) {
		s.logger.Error("id is not exists")
		return Question{}, errors.New("id is not exists")
	}

	return testQuestionList[questionID], nil
}

func (s *Service) UpdateQuestion(ctx context.Context, ID string, arg ReqParam) (Question, error) {
	// parse UUID logic, skip now...

	// temporary string convert logic
	questionID, err := strconv.Atoi(ID)
	if err != nil {
		s.logger.Error("failed to convert ID from string to int", zap.Error(err))
		return Question{}, err
	}

	if questionID >= len(testQuestionList) {
		s.logger.Error("id is not exists")
		return Question{}, errors.New("id is not exists")
	}

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
		ID:      strconv.Itoa(questionID),
		Type:    arg.Type,
		Content: arg.Content,
		Options: options,
	}

	testQuestionList[questionID] = question
	return testQuestionList[questionID], err
}
