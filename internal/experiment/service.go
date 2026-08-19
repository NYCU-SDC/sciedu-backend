package experiment

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Repository interface {
	List(ctx context.Context, params ListParams) ([]Record, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Create(ctx context.Context, params CreateParams) (Record, error)
	FindByID(ctx context.Context, id uuid.UUID) (Record, error)
	Update(ctx context.Context, params UpdateParams) (Record, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (Record, error)
}

type ListInput struct {
	Page          int32
	PageSize      int32
	Status        *Status
	ScheduledFrom *time.Time
	ScheduledTo   *time.Time
	Search        *string
}

type Page struct {
	Items       []Record
	TotalPages  int32
	TotalItems  int32
	CurrentPage int32
	PageSize    int32
	HasNextPage bool
}

type Service struct {
	repo   Repository
	logger *zap.Logger
}

func NewService(repo Repository, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{repo: repo, logger: logger}
}

func (s *Service) List(ctx context.Context, input ListInput) (Page, error) {
	filter := ListFilter{
		Status:        input.Status,
		ScheduledFrom: input.ScheduledFrom,
		ScheduledTo:   input.ScheduledTo,
		Search:        input.Search,
	}
	items, err := s.repo.List(ctx, ListParams{
		ListFilter: filter,
		Limit:      input.PageSize,
		Offset:     int64(input.Page-1) * int64(input.PageSize),
	})
	if err != nil {
		return Page{}, databaseutil.WrapDBError(err, s.logger, "list experiments")
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return Page{}, databaseutil.WrapDBError(err, s.logger, "count experiments")
	}
	var totalPages int32
	if total > 0 {
		totalPages = int32((total + int64(input.PageSize) - 1) / int64(input.PageSize))
	}
	return Page{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  int32(total),
		CurrentPage: input.Page,
		PageSize:    input.PageSize,
		HasNextPage: input.Page < totalPages,
	}, nil
}

func (s *Service) Create(ctx context.Context, createdBy uuid.UUID, params EditableParams) (Record, error) {
	params, err := validateEditableParams(params)
	if err != nil {
		return Record{}, err
	}
	record, err := s.repo.Create(ctx, CreateParams{CreatedBy: createdBy, EditableParams: params})
	if err != nil {
		return Record{}, databaseutil.WrapDBError(err, s.logger, "create experiment")
	}
	return record, nil
}

func (s *Service) FindByID(ctx context.Context, id uuid.UUID) (Record, error) {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return Record{}, databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", id.String(), s.logger, "get experiment")
	}
	return record, nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, params EditableParams) (Record, error) {
	params, err := validateEditableParams(params)
	if err != nil {
		return Record{}, err
	}
	record, err := s.repo.Update(ctx, UpdateParams{ID: id, EditableParams: params})
	if err != nil {
		return Record{}, databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", id.String(), s.logger, "update experiment")
	}
	return record, nil
}

func (s *Service) UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (Record, error) {
	if !status.Valid() {
		return Record{}, fmt.Errorf("%w: unknown experiment status", errInvalidExperimentPayload)
	}
	record, err := s.repo.UpdateStatus(ctx, id, status)
	if err != nil {
		return Record{}, databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", id.String(), s.logger, "update experiment status")
	}
	return record, nil
}

func validateEditableParams(params EditableParams) (EditableParams, error) {
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" || utf8.RuneCountInString(params.Name) > 200 {
		return EditableParams{}, fmt.Errorf("%w: name must contain between 1 and 200 characters", errInvalidExperimentPayload)
	}
	if params.Description != nil && utf8.RuneCountInString(*params.Description) > 4000 {
		return EditableParams{}, fmt.Errorf("%w: description must be at most 4000 characters", errInvalidExperimentPayload)
	}
	if params.ScheduledStartAt.IsZero() || params.ScheduledEndAt.IsZero() || !params.ScheduledEndAt.After(params.ScheduledStartAt) {
		return EditableParams{}, fmt.Errorf("%w: scheduledEndAt must be later than scheduledStartAt", errInvalidExperimentPayload)
	}
	if err := params.Configuration.Validate(); err != nil {
		return EditableParams{}, err
	}
	return params, nil
}

func (c Configuration) Validate() error {
	if c.AllowRetry && c.MaxAttempts < 2 {
		return fmt.Errorf("%w: maxAttempts must be at least 2 when allowRetry is true", errInvalidExperimentPayload)
	}
	if !c.AllowRetry && c.MaxAttempts != 1 {
		return fmt.Errorf("%w: maxAttempts must be 1 when allowRetry is false", errInvalidExperimentPayload)
	}
	if c.GradingMode != GradingModeAutomatic && c.GradingMode != GradingModeManual {
		return fmt.Errorf("%w: unknown gradingMode", errInvalidExperimentPayload)
	}
	switch c.CorrectAnswerReleaseMode {
	case CorrectAnswerReleaseAfterPageSubmission, CorrectAnswerReleaseAfterCourseCompletion, CorrectAnswerReleaseNever:
		return nil
	default:
		return fmt.Errorf("%w: unknown correctAnswerReleaseMode", errInvalidExperimentPayload)
	}
}

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusScheduled, StatusActive, StatusCompleted, StatusArchived:
		return true
	default:
		return false
	}
}
