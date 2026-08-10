package course

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"sciedu-backend/internal/auth"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

const (
	maxCourseCodeLength        = 100
	maxCourseTitleLength       = 200
	maxCourseDescriptionLength = 4000
)

type Repository interface {
	Create(ctx context.Context, params CreateParams) (Record, error)
	List(ctx context.Context, params ListParams) ([]Record, error)
	Count(ctx context.Context, filter ListFilter) (int64, error)
	ByID(ctx context.Context, id uuid.UUID) (Record, error)
	Update(ctx context.Context, params UpdateParams) (Record, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status CourseStatus) (Record, error)
}

// StudentCourseAccessChecker determines whether a student may read a course.
// The Experiment domain will provide the production implementation once it is available.
type StudentCourseAccessChecker interface {
	CanAccessCourse(ctx context.Context, studentID, courseID uuid.UUID) (bool, error)
}

type ListInput struct {
	Page     int32
	PageSize int32
	Status   *CourseStatus
	Search   *string
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
	logger        *zap.Logger
	repo          Repository
	roles         auth.RoleQuerier
	studentAccess StudentCourseAccessChecker
}

func NewService(repo Repository, roles auth.RoleQuerier, studentAccess StudentCourseAccessChecker, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{repo: repo, roles: roles, studentAccess: studentAccess, logger: logger}
}

func (s *Service) List(ctx context.Context, input ListInput) (Page, error) {
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > maxPageSize {
		return Page{}, fmt.Errorf("%w: page must be positive and pageSize must be between 1 and %d", errInvalidCoursePayload, maxPageSize)
	}
	if input.Status != nil && !validCourseStatus(*input.Status) {
		return Page{}, fmt.Errorf("%w: unknown course status", errInvalidCoursePayload)
	}
	if input.Search != nil && (utf8.RuneCountInString(*input.Search) < 1 || utf8.RuneCountInString(*input.Search) > maxCourseTitleLength) {
		return Page{}, fmt.Errorf("%w: search must be between 1 and %d characters", errInvalidCoursePayload, maxCourseTitleLength)
	}
	offset := (int64(input.Page) - 1) * int64(input.PageSize)
	if offset > math.MaxInt32 {
		return Page{}, fmt.Errorf("%w: page offset is too large", errInvalidCoursePayload)
	}

	filter := ListFilter{Status: input.Status, Search: input.Search}
	items, err := s.repo.List(ctx, ListParams{
		ListFilter: filter,
		Limit:      input.PageSize,
		Offset:     int32(offset),
	})
	if err != nil {
		return Page{}, databaseutil.WrapDBError(err, s.logger, "list courses")
	}

	total, err := s.repo.Count(ctx, filter)
	if err != nil {
		return Page{}, databaseutil.WrapDBError(err, s.logger, "count courses")
	}
	if total > math.MaxInt32 {
		return Page{}, fmt.Errorf("%w: course count exceeds the supported range", errInvalidCoursePayload)
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

func (s *Service) Create(ctx context.Context, params CreateParams) (Record, error) {
	if err := validateCourseMetadata(params.Code, params.Title, params.Description); err != nil {
		return Record{}, err
	}

	record, err := s.repo.Create(ctx, params)
	if err != nil {
		return Record{}, databaseutil.WrapDBError(err, s.logger, "create course")
	}
	return record, nil
}

func (s *Service) ByID(ctx context.Context, id uuid.UUID) (Record, error) {
	record, err := s.repo.ByID(ctx, id)
	if err != nil {
		return Record{}, databaseutil.WrapDBErrorWithKeyValue(err, "courses", "id", id.String(), s.logger, "get course")
	}
	return record, nil
}

func (s *Service) ByIDForActor(ctx context.Context, actorID, courseID uuid.UUID) (Record, error) {
	if s.roles == nil {
		return Record{}, errors.New("course role querier is unavailable")
	}

	roles, err := s.roles.ActiveUserRoles(ctx, actorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Record{}, handlerutil.ErrUnauthorized
		}
		return Record{}, databaseutil.WrapDBError(err, s.logger, "get course actor roles")
	}

	if hasCourseRole(roles, auth.EXPERIMENTER) || hasCourseRole(roles, auth.ADMIN) {
		return s.ByID(ctx, courseID)
	}
	if !hasCourseRole(roles, auth.STUDENT) {
		return Record{}, handlerutil.ErrForbidden
	}
	if s.studentAccess == nil {
		return Record{}, errors.New("student course access checker is unavailable")
	}

	allowed, err := s.studentAccess.CanAccessCourse(ctx, actorID, courseID)
	if err != nil {
		return Record{}, fmt.Errorf("check student course access: %w", err)
	}
	if !allowed {
		return Record{}, handlerutil.ErrForbidden
	}
	return s.ByID(ctx, courseID)
}

func (s *Service) Update(ctx context.Context, params UpdateParams) (Record, error) {
	if err := validateCourseMetadata(params.Code, params.Title, params.Description); err != nil {
		return Record{}, err
	}

	record, err := s.repo.Update(ctx, params)
	if err != nil {
		return Record{}, databaseutil.WrapDBErrorWithKeyValue(err, "courses", "id", params.ID.String(), s.logger, "update course")
	}
	return record, nil
}

func (s *Service) UpdateStatus(ctx context.Context, id uuid.UUID, status CourseStatus) (Record, error) {
	if !validCourseStatus(status) {
		return Record{}, fmt.Errorf("%w: unknown course status", errInvalidCoursePayload)
	}

	record, err := s.repo.UpdateStatus(ctx, id, status)
	if err != nil {
		return Record{}, databaseutil.WrapDBErrorWithKeyValue(err, "courses", "id", id.String(), s.logger, "update course status")
	}
	return record, nil
}

func validateCourseMetadata(code, title string, description *string) error {
	if strings.TrimSpace(code) == "" || utf8.RuneCountInString(code) > maxCourseCodeLength {
		return fmt.Errorf("%w: code must be between 1 and %d characters", errInvalidCoursePayload, maxCourseCodeLength)
	}
	if strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > maxCourseTitleLength {
		return fmt.Errorf("%w: title must be between 1 and %d characters", errInvalidCoursePayload, maxCourseTitleLength)
	}
	if description != nil && utf8.RuneCountInString(*description) > maxCourseDescriptionLength {
		return fmt.Errorf("%w: description must be at most %d characters", errInvalidCoursePayload, maxCourseDescriptionLength)
	}
	return nil
}

func validCourseStatus(status CourseStatus) bool {
	switch status {
	case CourseStatusDRAFT, CourseStatusPUBLISHED, CourseStatusARCHIVED:
		return true
	default:
		return false
	}
}

func hasCourseRole(roles []auth.Role, want auth.Role) bool {
	for _, role := range roles {
		if role == want {
			return true
		}
	}
	return false
}
