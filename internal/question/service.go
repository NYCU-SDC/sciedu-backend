package question

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidQuestion = errors.New("invalid question")

type Store interface {
	List(ctx context.Context) ([]Question, error)
	Get(ctx context.Context, id uuid.UUID) (Question, error)
	Create(ctx context.Context, req UpsertRequest) (Question, error)
	Update(ctx context.Context, id uuid.UUID, req UpsertRequest) (Question, error)
	Delete(ctx context.Context, id uuid.UUID) error
	ListAnswers(ctx context.Context, questionID uuid.UUID) ([]Answer, error)
	CreateAnswer(ctx context.Context, questionID uuid.UUID, req SubmitAnswerRequest) (Answer, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) List(ctx context.Context) ([]Question, error) {
	return s.store.List(ctx)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Question, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) Create(ctx context.Context, req UpsertRequest) (Question, error) {
	if err := validateQuestion(req); err != nil {
		return Question{}, err
	}
	return s.store.Create(ctx, req)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req UpsertRequest) (Question, error) {
	if err := validateQuestion(req); err != nil {
		return Question{}, err
	}
	return s.store.Update(ctx, id, req)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.store.Delete(ctx, id)
}

func (s *Service) ListAnswers(ctx context.Context, questionID uuid.UUID) ([]Answer, error) {
	return s.store.ListAnswers(ctx, questionID)
}

func (s *Service) SubmitAnswer(ctx context.Context, questionID uuid.UUID, req SubmitAnswerRequest) (Answer, error) {
	if req.SelectedOptionID == nil && (req.TextAnswer == nil || strings.TrimSpace(*req.TextAnswer) == "") {
		return Answer{}, fmt.Errorf("%w: answer content is required", ErrInvalidQuestion)
	}
	if req.SelectedOptionID != nil && req.TextAnswer != nil && strings.TrimSpace(*req.TextAnswer) != "" {
		return Answer{}, fmt.Errorf("%w: provide selectedOptionId or textAnswer, not both", ErrInvalidQuestion)
	}
	return s.store.CreateAnswer(ctx, questionID, req)
}

func validateQuestion(req UpsertRequest) error {
	if req.Type != TypeChoice && req.Type != TypeText {
		return fmt.Errorf("%w: type must be CHOICE or TEXT", ErrInvalidQuestion)
	}
	if strings.TrimSpace(req.Content) == "" {
		return fmt.Errorf("%w: content is required", ErrInvalidQuestion)
	}
	if req.Type == TypeText && len(req.Options) > 0 {
		return fmt.Errorf("%w: TEXT question cannot have options", ErrInvalidQuestion)
	}
	if req.Type == TypeChoice && len(req.Options) == 0 {
		return fmt.Errorf("%w: CHOICE question requires options", ErrInvalidQuestion)
	}
	labels := make(map[string]struct{}, len(req.Options))
	for _, option := range req.Options {
		if strings.TrimSpace(option.Label) == "" || strings.TrimSpace(option.Content) == "" {
			return fmt.Errorf("%w: option label and content are required", ErrInvalidQuestion)
		}
		if _, exists := labels[option.Label]; exists {
			return fmt.Errorf("%w: option labels must be unique", ErrInvalidQuestion)
		}
		labels[option.Label] = struct{}{}
	}
	return nil
}
