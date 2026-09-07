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
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type MutationRepository interface {
	LockByID(ctx context.Context, id uuid.UUID) (Record, error)
	LockParticipantUsers(ctx context.Context, experimentID uuid.UUID) ([]uuid.UUID, error)
	HasParticipantScheduleConflict(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID, start, end time.Time) (bool, error)
	Update(ctx context.Context, params UpdateParams) (Record, error)
}

type Repository interface {
	List(ctx context.Context, params ListParams) ([]Record, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Create(ctx context.Context, params CreateParams) (Record, error)
	FindByID(ctx context.Context, id uuid.UUID) (Record, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (Record, error)
	WithinTx(ctx context.Context, fn func(MutationRepository) error) error
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

	var record Record
	err = s.repo.WithinTx(ctx, func(repo MutationRepository) error {
		current, err := repo.LockByID(ctx, id)
		if err != nil {
			return databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", id.String(), s.logger, "lock experiment for update")
		}
		if err := validateLifecycleUpdate(current, params); err != nil {
			return err
		}
		if scheduleChanged(current, params) {
			if err := s.ensureParticipantScheduleAvailable(ctx, repo, current.ID, params.ScheduledStartAt, params.ScheduledEndAt); err != nil {
				return err
			}
		}

		record, err = repo.Update(ctx, UpdateParams{ID: id, EditableParams: params})
		if err != nil {
			return databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", id.String(), s.logger, "update experiment")
		}
		return nil
	})
	if err != nil {
		if isMappedMutationError(err) {
			return Record{}, err
		}
		return Record{}, databaseutil.WrapDBError(err, s.logger, "update experiment transaction")
	}
	return record, nil
}

func isMappedMutationError(err error) bool {
	var internalError databaseutil.InternalServerError
	return errors.Is(err, errExperimentConflict) ||
		errors.Is(err, errInvalidExperimentPayload) ||
		errors.Is(err, handlerutil.ErrNotFound) ||
		errors.Is(err, databaseutil.ErrUniqueViolation) ||
		errors.Is(err, databaseutil.ErrForeignKeyViolation) ||
		errors.Is(err, databaseutil.ErrDeadlockDetected) ||
		errors.Is(err, databaseutil.ErrQueryTimeout) ||
		errors.As(err, &internalError)
}

func validateLifecycleUpdate(current Record, params EditableParams) error {
	switch current.Status {
	case StatusDraft, StatusScheduled:
		return nil
	case StatusActive:
		if current.Name != params.Name ||
			!stringPointersEqual(current.Description, params.Description) ||
			current.Configuration != params.Configuration ||
			!current.ScheduledStartAt.Equal(params.ScheduledStartAt) ||
			params.ScheduledEndAt.Before(current.ScheduledEndAt) {
			return fmt.Errorf("%w: ACTIVE experiments only allow scheduledEndAt to remain unchanged or move later", errExperimentConflict)
		}
		return nil
	case StatusCompleted, StatusArchived:
		return fmt.Errorf("%w: %s experiments are read only", errExperimentConflict, current.Status)
	default:
		return fmt.Errorf("%w: unknown persisted experiment status %q", errExperimentConflict, current.Status)
	}
}

func scheduleChanged(current Record, params EditableParams) bool {
	return !current.ScheduledStartAt.Equal(params.ScheduledStartAt) ||
		!current.ScheduledEndAt.Equal(params.ScheduledEndAt)
}

func stringPointersEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (s *Service) ensureParticipantScheduleAvailable(
	ctx context.Context,
	repo MutationRepository,
	experimentID uuid.UUID,
	start time.Time,
	end time.Time,
) error {
	userIDs, err := repo.LockParticipantUsers(ctx, experimentID)
	if err != nil {
		return databaseutil.WrapDBError(err, s.logger, "lock experiment participants")
	}
	if len(userIDs) == 0 {
		return nil
	}

	conflict, err := repo.HasParticipantScheduleConflict(ctx, experimentID, userIDs, start, end)
	if err != nil {
		return databaseutil.WrapDBError(err, s.logger, "check participant schedule conflicts")
	}
	if conflict {
		return fmt.Errorf("%w: updated schedule overlaps another experiment assigned to a participant", errExperimentConflict)
	}
	return nil
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
