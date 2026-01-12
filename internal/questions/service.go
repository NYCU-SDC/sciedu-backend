package questions

import (
	"context"
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

type CreateParam struct {
	Type       string
	Content    string
	ReqOptions []ReqOption
}

type Querier interface {
	Create(ctx context.Context, arg CreateParam) (Question, error)
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

func (s *Service) Create(ctx context.Context, arg CreateParam) (Question, error) {
	// handle mock ID for each ReqOption
	var options []Option
	for i, option := range arg.ReqOptions {
		options = append(options, Option{
			ID:      strconv.Itoa(i),
			Label:   option.Label,
			Content: option.Content,
		})
	}

	res := Question{
		ID:      strconv.Itoa(len(testQuestionList)),
		Type:    arg.Type,
		Content: arg.Content,
		Options: options,
	}

	testQuestionList = append(testQuestionList, res)

	// here should have some error handle after connected the database...

	return res, nil
}

func (s *Service) List(ctx context.Context) ([]Question, error) {
	return testQuestionList, nil
}
