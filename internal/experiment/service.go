package experiment

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

const maxParticipantBatchSize = 100

type Repository interface {
	List(ctx context.Context, params ListParams) ([]Record, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Create(ctx context.Context, params CreateParams) (Record, error)
	FindByID(ctx context.Context, id uuid.UUID) (Record, error)
	Update(ctx context.Context, params UpdateParams) (Record, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (Record, error)
	ListParticipants(ctx context.Context, params ParticipantListParams) ([]ParticipantAssignment, int64, error)
	AddParticipants(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID) ([]ParticipantAssignment, error)
	RemoveParticipant(ctx context.Context, experimentID, userID uuid.UUID) error
}

type ListInput struct {
	Page          int32
	PageSize      int32
	Status        *Status
	ScheduledFrom *time.Time
	ScheduledTo   *time.Time
	Search        *string
}

type ExperimentPage struct {
	Items       []Record
	TotalPages  int32
	TotalItems  int32
	CurrentPage int32
	PageSize    int32
	HasNextPage bool
}

type ParticipantListInput struct {
	Page     int32
	PageSize int32
}

type ParticipantPage struct {
	Items       []ParticipantAssignment
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

func (s *Service) List(ctx context.Context, input ListInput) (ExperimentPage, error) {
	if err := validateListInput(input); err != nil {
		return ExperimentPage{}, err
	}

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
		return ExperimentPage{}, databaseutil.WrapDBError(err, s.logger, "list experiments")
	}
	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return ExperimentPage{}, databaseutil.WrapDBError(err, s.logger, "count experiments")
	}
	if total < 0 || total > math.MaxInt32 {
		return ExperimentPage{}, fmt.Errorf("%w: experiment count exceeds the supported range", errInvalidExperimentPayload)
	}
	var totalPages int32
	if total > 0 {
		totalPages = int32((total + int64(input.PageSize) - 1) / int64(input.PageSize))
	}
	return ExperimentPage{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  int32(total),
		CurrentPage: input.Page,
		PageSize:    input.PageSize,
		HasNextPage: input.Page < totalPages,
	}, nil
}

func validateListInput(input ListInput) error {
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > maxPageSize {
		return fmt.Errorf("%w: page must be positive and pageSize must be between 1 and %d", errInvalidExperimentPayload, maxPageSize)
	}
	if input.Status != nil && !input.Status.Valid() {
		return fmt.Errorf("%w: unknown experiment status", errInvalidExperimentPayload)
	}
	if input.Search != nil {
		length := utf8.RuneCountInString(*input.Search)
		if length < 1 || length > maxSearchLength {
			return fmt.Errorf("%w: search must contain between 1 and %d characters", errInvalidExperimentPayload, maxSearchLength)
		}
	}

	return nil
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

func (s *Service) ListParticipants(
	ctx context.Context,
	experimentID uuid.UUID,
	input ParticipantListInput,
) (ParticipantPage, error) {
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > maxPageSize {
		return ParticipantPage{}, fmt.Errorf(
			"%w: page must be positive and pageSize must be between 1 and %d",
			errInvalidExperimentPayload,
			maxPageSize,
		)
	}

	items, total, err := s.repo.ListParticipants(ctx, ParticipantListParams{
		ExperimentID: experimentID,
		Limit:        input.PageSize,
		Offset:       int64(input.Page-1) * int64(input.PageSize),
	})
	if err != nil {
		return ParticipantPage{}, databaseutil.WrapDBErrorWithKeyValue(
			err,
			"experiments",
			"id",
			experimentID.String(),
			s.logger,
			"list experiment participants",
		)
	}
	if total < 0 || total > math.MaxInt32 {
		return ParticipantPage{}, fmt.Errorf("%w: participant count exceeds the supported range", errInvalidExperimentPayload)
	}

	var totalPages int32
	if total > 0 {
		totalPages = int32((total + int64(input.PageSize) - 1) / int64(input.PageSize))
	}
	return ParticipantPage{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  int32(total),
		CurrentPage: input.Page,
		PageSize:    input.PageSize,
		HasNextPage: input.Page < totalPages,
	}, nil
}

func (s *Service) AddParticipants(
	ctx context.Context,
	experimentID uuid.UUID,
	userIDs []uuid.UUID,
) ([]ParticipantAssignment, error) {
	if len(userIDs) < 1 || len(userIDs) > maxParticipantBatchSize {
		return nil, fmt.Errorf(
			"%w: userIds must contain between 1 and %d items",
			errInvalidExperimentPayload,
			maxParticipantBatchSize,
		)
	}
	if hasDuplicateUUIDs(userIDs) {
		return nil, fmt.Errorf("%w: userIds contains duplicate assignments", errExperimentParticipantConflict)
	}

	assignments, err := s.repo.AddParticipants(ctx, experimentID, userIDs)
	if err != nil {
		if errors.Is(err, errExperimentParticipantConflict) {
			return nil, fmt.Errorf("%w: %v", errExperimentParticipantConflict, err)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, databaseutil.WrapDBErrorWithKeyValue(
				err,
				"experiments",
				"id",
				experimentID.String(),
				s.logger,
				"add experiment participants",
			)
		}
		wrapped := databaseutil.WrapDBError(err, s.logger, "add experiment participants")
		if errors.Is(wrapped, databaseutil.ErrUniqueViolation) ||
			errors.Is(wrapped, databaseutil.ErrForeignKeyViolation) {
			return nil, fmt.Errorf("%w: assignment changed concurrently", errExperimentParticipantConflict)
		}
		return nil, wrapped
	}
	return assignments, nil
}

func (s *Service) RemoveParticipant(ctx context.Context, experimentID, userID uuid.UUID) error {
	if err := s.repo.RemoveParticipant(ctx, experimentID, userID); err != nil {
		return databaseutil.WrapDBErrorWithKeyValue(
			err,
			"experiments",
			"id",
			experimentID.String(),
			s.logger,
			"remove experiment participant",
		)
	}
	return nil
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

func hasDuplicateUUIDs(ids []uuid.UUID) bool {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			return true
		}
		seen[id] = struct{}{}
	}
	return false
}
