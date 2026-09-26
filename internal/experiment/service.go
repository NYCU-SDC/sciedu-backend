package experiment

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"sciedu-backend/internal/auth"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

type MutationRepository interface {
	LockByID(ctx context.Context, id uuid.UUID) (Record, error)
	LockParticipantUsers(ctx context.Context, experimentID uuid.UUID) ([]uuid.UUID, error)
	LockParticipantCandidates(ctx context.Context, userIDs []uuid.UUID) ([]Participant, error)
	LockCourseCandidates(ctx context.Context, courseIDs []uuid.UUID) ([]AssignedCourse, error)
	HasParticipantScheduleConflict(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID, start, end time.Time) (bool, error)
	AddParticipants(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID) ([]ParticipantAssignment, error)
	RemoveParticipant(ctx context.Context, experimentID, userID uuid.UUID) error
	AddCourses(ctx context.Context, experimentID uuid.UUID, courseIDs []uuid.UUID) ([]CourseAssignment, error)
	RemoveCourse(ctx context.Context, experimentID, courseID uuid.UUID) error
	Update(ctx context.Context, params UpdateParams) (Record, error)
}

type Repository interface {
	List(ctx context.Context, params ListParams) ([]Record, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	Create(ctx context.Context, params CreateParams) (Record, error)
	FindByID(ctx context.Context, id uuid.UUID) (Record, error)
	ListParticipants(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]ParticipantAssignment, error)
	CountParticipants(ctx context.Context, experimentID uuid.UUID) (int64, error)
	ListCourses(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]CourseAssignment, error)
	CountCourses(ctx context.Context, experimentID uuid.UUID) (int64, error)
	StudentExperimentAccessible(ctx context.Context, experimentID, studentID uuid.UUID) (bool, error)
	ListStudentCourses(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]CourseAssignment, error)
	CountStudentCourses(ctx context.Context, experimentID uuid.UUID) (int64, error)
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

type ParticipantPage struct {
	Items       []ParticipantAssignment
	TotalPages  int32
	TotalItems  int32
	CurrentPage int32
	PageSize    int32
	HasNextPage bool
}

type CourseAssignmentPage struct {
	Items       []CourseAssignment
	TotalPages  int32
	TotalItems  int32
	CurrentPage int32
	PageSize    int32
	HasNextPage bool
}

type Service struct {
	repo   Repository
	roles  auth.RoleQuerier
	logger *zap.Logger
}

func NewService(repo Repository, logger *zap.Logger) *Service {
	return NewServiceWithRoles(repo, nil, logger)
}

func NewServiceWithRoles(repo Repository, roles auth.RoleQuerier, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{repo: repo, roles: roles, logger: logger}
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

func (s *Service) ListParticipants(ctx context.Context, experimentID uuid.UUID, page, pageSize int32) (ParticipantPage, error) {
	if err := validateAssignmentPagination(page, pageSize); err != nil {
		return ParticipantPage{}, err
	}
	if _, err := s.FindByID(ctx, experimentID); err != nil {
		return ParticipantPage{}, err
	}

	items, err := s.repo.ListParticipants(ctx, experimentID, pageSize, int64(page-1)*int64(pageSize))
	if err != nil {
		return ParticipantPage{}, databaseutil.WrapDBError(err, s.logger, "list experiment participants")
	}
	total, err := s.repo.CountParticipants(ctx, experimentID)
	if err != nil {
		return ParticipantPage{}, databaseutil.WrapDBError(err, s.logger, "count experiment participants")
	}
	if total < 0 || total > math.MaxInt32 {
		return ParticipantPage{}, fmt.Errorf("%w: participant count exceeds the supported range", errInvalidExperimentPayload)
	}

	var totalPages int32
	if total > 0 {
		totalPages = int32((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return ParticipantPage{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  int32(total),
		CurrentPage: page,
		PageSize:    pageSize,
		HasNextPage: page < totalPages,
	}, nil
}

func (s *Service) AddParticipants(ctx context.Context, experimentID uuid.UUID, requestedIDs []uuid.UUID) ([]ParticipantAssignment, error) {
	if len(requestedIDs) < 1 || len(requestedIDs) > 100 {
		return nil, fmt.Errorf("%w: userIds must contain between 1 and 100 items", errInvalidExperimentPayload)
	}

	userIDs := append([]uuid.UUID(nil), requestedIDs...)
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i].String() < userIDs[j].String() })
	for i := 1; i < len(userIDs); i++ {
		if userIDs[i] == userIDs[i-1] {
			return nil, fmt.Errorf("%w: duplicate participant id %s", errExperimentConflict, userIDs[i])
		}
	}

	var assignments []ParticipantAssignment
	err := s.repo.WithinTx(ctx, func(repo MutationRepository) error {
		experiment, err := repo.LockByID(ctx, experimentID)
		if err != nil {
			return databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", experimentID.String(), s.logger, "lock experiment for participant assignment")
		}
		if err := validateAssignmentAdd(experiment.Status); err != nil {
			return err
		}

		participants, err := repo.LockParticipantCandidates(ctx, userIDs)
		if err != nil {
			return databaseutil.WrapDBError(err, s.logger, "lock participant candidates")
		}
		if len(participants) != len(userIDs) {
			return fmt.Errorf("%w: one or more participants do not exist", errExperimentConflict)
		}
		for _, participant := range participants {
			if participant.DisabledAt != nil || !hasString(participant.Roles, "STUDENT") {
				return fmt.Errorf("%w: participant %s must be an active STUDENT", errExperimentConflict, participant.ID)
			}
		}

		conflict, err := repo.HasParticipantScheduleConflict(
			ctx,
			experiment.ID,
			userIDs,
			experiment.ScheduledStartAt,
			experiment.ScheduledEndAt,
		)
		if err != nil {
			return databaseutil.WrapDBError(err, s.logger, "check participant schedule conflicts")
		}
		if conflict {
			return fmt.Errorf("%w: participant schedule overlaps another experiment", errExperimentConflict)
		}

		assignments, err = repo.AddParticipants(ctx, experiment.ID, userIDs)
		if err != nil {
			wrapped := databaseutil.WrapDBError(err, s.logger, "add experiment participants")
			if errors.Is(wrapped, databaseutil.ErrUniqueViolation) || errors.Is(wrapped, databaseutil.ErrForeignKeyViolation) {
				return fmt.Errorf("%w: one or more participant assignments conflict", errExperimentConflict)
			}
			return wrapped
		}
		return nil
	})
	if err != nil {
		if isMappedMutationError(err) {
			return nil, err
		}
		return nil, databaseutil.WrapDBError(err, s.logger, "add experiment participants transaction")
	}
	return assignments, nil
}

func (s *Service) RemoveParticipant(ctx context.Context, experimentID, userID uuid.UUID) error {
	err := s.repo.WithinTx(ctx, func(repo MutationRepository) error {
		experiment, err := repo.LockByID(ctx, experimentID)
		if err != nil {
			return databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", experimentID.String(), s.logger, "lock experiment for participant removal")
		}
		if err := validateAssignmentRemove(experiment.Status); err != nil {
			return err
		}
		if err := repo.RemoveParticipant(ctx, experiment.ID, userID); err != nil {
			return databaseutil.WrapDBError(err, s.logger, "remove experiment participant")
		}
		return nil
	})
	if err == nil || isMappedMutationError(err) {
		return err
	}
	return databaseutil.WrapDBError(err, s.logger, "remove experiment participant transaction")
}

func (s *Service) ListCoursesForActor(
	ctx context.Context,
	actorID uuid.UUID,
	experimentID uuid.UUID,
	page int32,
	pageSize int32,
) (CourseAssignmentPage, error) {
	if err := validateAssignmentPagination(page, pageSize); err != nil {
		return CourseAssignmentPage{}, err
	}
	if s.roles == nil {
		return CourseAssignmentPage{}, errors.New("experiment role querier is unavailable")
	}
	roles, err := s.roles.ActiveUserRoles(ctx, actorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CourseAssignmentPage{}, handlerutil.ErrUnauthorized
		}
		return CourseAssignmentPage{}, databaseutil.WrapDBError(err, s.logger, "get experiment course actor roles")
	}
	isManagement := hasRole(roles, auth.EXPERIMENTER) || hasRole(roles, auth.ADMIN)
	if !isManagement && !hasRole(roles, auth.STUDENT) {
		return CourseAssignmentPage{}, handlerutil.ErrForbidden
	}
	if _, err := s.FindByID(ctx, experimentID); err != nil {
		return CourseAssignmentPage{}, err
	}

	offset := int64(page-1) * int64(pageSize)
	if isManagement {
		items, err := s.repo.ListCourses(ctx, experimentID, pageSize, offset)
		if err != nil {
			return CourseAssignmentPage{}, databaseutil.WrapDBError(err, s.logger, "list experiment courses")
		}
		total, err := s.repo.CountCourses(ctx, experimentID)
		if err != nil {
			return CourseAssignmentPage{}, databaseutil.WrapDBError(err, s.logger, "count experiment courses")
		}
		return coursePage(items, total, page, pageSize)
	}

	accessible, err := s.repo.StudentExperimentAccessible(ctx, experimentID, actorID)
	if err != nil {
		return CourseAssignmentPage{}, databaseutil.WrapDBError(err, s.logger, "check student experiment access")
	}
	if !accessible {
		return CourseAssignmentPage{}, handlerutil.ErrForbidden
	}
	items, err := s.repo.ListStudentCourses(ctx, experimentID, pageSize, offset)
	if err != nil {
		return CourseAssignmentPage{}, databaseutil.WrapDBError(err, s.logger, "list student experiment courses")
	}
	total, err := s.repo.CountStudentCourses(ctx, experimentID)
	if err != nil {
		return CourseAssignmentPage{}, databaseutil.WrapDBError(err, s.logger, "count student experiment courses")
	}
	return coursePage(items, total, page, pageSize)
}

func (s *Service) AddCourses(ctx context.Context, experimentID uuid.UUID, requestedIDs []uuid.UUID) ([]CourseAssignment, error) {
	if len(requestedIDs) < 1 || len(requestedIDs) > 100 {
		return nil, fmt.Errorf("%w: courseIds must contain between 1 and 100 items", errInvalidExperimentPayload)
	}

	courseIDs := append([]uuid.UUID(nil), requestedIDs...)
	sort.Slice(courseIDs, func(i, j int) bool { return courseIDs[i].String() < courseIDs[j].String() })
	for i := 1; i < len(courseIDs); i++ {
		if courseIDs[i] == courseIDs[i-1] {
			return nil, fmt.Errorf("%w: duplicate Course id %s", errExperimentConflict, courseIDs[i])
		}
	}

	var assignments []CourseAssignment
	err := s.repo.WithinTx(ctx, func(repo MutationRepository) error {
		experiment, err := repo.LockByID(ctx, experimentID)
		if err != nil {
			return databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", experimentID.String(), s.logger, "lock experiment for Course assignment")
		}
		if err := validateAssignmentAdd(experiment.Status); err != nil {
			return err
		}

		courses, err := repo.LockCourseCandidates(ctx, courseIDs)
		if err != nil {
			return databaseutil.WrapDBError(err, s.logger, "lock Course candidates")
		}
		if len(courses) != len(courseIDs) {
			return fmt.Errorf("%w: one or more Courses do not exist", errExperimentConflict)
		}
		for _, course := range courses {
			if course.Status != CourseStatusDRAFT && course.Status != CourseStatusPUBLISHED {
				return fmt.Errorf("%w: Course %s is not assignable", errExperimentConflict, course.ID)
			}
		}

		assignments, err = repo.AddCourses(ctx, experiment.ID, courseIDs)
		if err != nil {
			wrapped := databaseutil.WrapDBError(err, s.logger, "add experiment Courses")
			if errors.Is(wrapped, databaseutil.ErrUniqueViolation) || errors.Is(wrapped, databaseutil.ErrForeignKeyViolation) {
				return fmt.Errorf("%w: one or more Course assignments conflict", errExperimentConflict)
			}
			return wrapped
		}
		return nil
	})
	if err != nil {
		if isMappedMutationError(err) {
			return nil, err
		}
		return nil, databaseutil.WrapDBError(err, s.logger, "add experiment Courses transaction")
	}
	return assignments, nil
}

func (s *Service) RemoveCourse(ctx context.Context, experimentID, courseID uuid.UUID) error {
	err := s.repo.WithinTx(ctx, func(repo MutationRepository) error {
		experiment, err := repo.LockByID(ctx, experimentID)
		if err != nil {
			return databaseutil.WrapDBErrorWithKeyValue(err, "experiments", "id", experimentID.String(), s.logger, "lock experiment for Course removal")
		}
		if err := validateAssignmentRemove(experiment.Status); err != nil {
			return err
		}
		if err := repo.RemoveCourse(ctx, experiment.ID, courseID); err != nil {
			return databaseutil.WrapDBError(err, s.logger, "remove experiment Course")
		}
		return nil
	})
	if err == nil || isMappedMutationError(err) {
		return err
	}
	return databaseutil.WrapDBError(err, s.logger, "remove experiment Course transaction")
}

func coursePage(items []CourseAssignment, total int64, page, pageSize int32) (CourseAssignmentPage, error) {
	if total < 0 || total > math.MaxInt32 {
		return CourseAssignmentPage{}, fmt.Errorf("%w: Course assignment count exceeds the supported range", errInvalidExperimentPayload)
	}
	var totalPages int32
	if total > 0 {
		totalPages = int32((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return CourseAssignmentPage{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  int32(total),
		CurrentPage: page,
		PageSize:    pageSize,
		HasNextPage: page < totalPages,
	}, nil
}

func hasRole(roles []auth.Role, want auth.Role) bool {
	for _, role := range roles {
		if role == want {
			return true
		}
	}
	return false
}

func validateAssignmentPagination(page, pageSize int32) error {
	if page < 1 || pageSize < 1 || pageSize > maxPageSize {
		return fmt.Errorf("%w: page must be positive and pageSize must be between 1 and %d", errInvalidExperimentPayload, maxPageSize)
	}
	return nil
}

func validateAssignmentAdd(status Status) error {
	switch status {
	case StatusDraft, StatusScheduled, StatusActive:
		return nil
	case StatusCompleted, StatusArchived:
		return fmt.Errorf("%w: %s experiments do not allow new assignments", errExperimentConflict, status)
	default:
		return fmt.Errorf("%w: unknown persisted experiment status %q", errExperimentConflict, status)
	}
}

func validateAssignmentRemove(status Status) error {
	switch status {
	case StatusDraft, StatusScheduled:
		return nil
	case StatusActive, StatusCompleted, StatusArchived:
		return fmt.Errorf("%w: %s experiments do not allow assignment removal", errExperimentConflict, status)
	default:
		return fmt.Errorf("%w: unknown persisted experiment status %q", errExperimentConflict, status)
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
