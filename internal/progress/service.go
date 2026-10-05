package progress

import (
	"context"
	"errors"
	"fmt"
	"time"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Repository interface {
	PageByID(ctx context.Context, pageID uuid.UUID) (PageInfo, error)
	PagesByCourse(ctx context.Context, courseID uuid.UUID) ([]PageInfo, error)
	ProgressByStudentCourse(ctx context.Context, studentID, courseID uuid.UUID) ([]ProgressRow, error)
	UpsertReach(ctx context.Context, studentID, pageID uuid.UUID, reachedAt time.Time) (ProgressRow, error)
	UpsertComplete(ctx context.Context, studentID, pageID uuid.UUID, completedAt time.Time) (ProgressRow, error)

	ListCourseParticipants(ctx context.Context, experimentID uuid.UUID, limit int32, offset int64) ([]Participant, error)
	CountCourseParticipants(ctx context.Context, experimentID uuid.UUID) (int64, error)
	ProgressForParticipants(ctx context.Context, courseID uuid.UUID, studentIDs []uuid.UUID) (map[uuid.UUID][]ProgressRow, error)
	ExperimentCourseExists(ctx context.Context, experimentID, courseID uuid.UUID) (bool, error)
	ParticipantInExperiment(ctx context.Context, experimentID, studentID uuid.UUID) (bool, error)
}

type CurrentExperimentFinder interface {
	CurrentExperimentForStudent(ctx context.Context, studentID uuid.UUID) (uuid.UUID, bool, error)
}

type CourseAccessDecision struct {
	Found   bool
	Allowed bool
}

type StudentCourseAccessChecker interface {
	CheckStudentCourseAccess(ctx context.Context, studentID, courseID uuid.UUID) (CourseAccessDecision, error)
}

type Clock func() time.Time

type Service struct {
	repo       Repository
	experiment CurrentExperimentFinder
	access     StudentCourseAccessChecker
	now        Clock
	logger     *zap.Logger
}

func NewService(repo Repository, experiment CurrentExperimentFinder, access StudentCourseAccessChecker, now Clock, logger *zap.Logger) *Service {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		repo:       repo,
		experiment: experiment,
		access:     access,
		now:        now,
		logger:     logger,
	}
}

func (s *Service) GetMyCourseProgress(ctx context.Context, studentID, courseID uuid.UUID) (CourseProgressDetail, error) {
	expID, err := s.authorizeStudentCourse(ctx, studentID, courseID)
	if err != nil {
		return CourseProgressDetail{}, err
	}
	return s.buildDetail(ctx, studentID, expID, courseID)
}

func (s *Service) ReachPage(ctx context.Context, studentID, pageID uuid.UUID) (CourseProgressDetail, error) {
	courseID, err := s.resolvePageCourse(ctx, pageID)
	if err != nil {
		return CourseProgressDetail{}, err
	}
	expID, err := s.authorizeStudentCourse(ctx, studentID, courseID)
	if err != nil {
		return CourseProgressDetail{}, err
	}
	if _, err := s.repo.UpsertReach(ctx, studentID, pageID, s.now().UTC()); err != nil {
		return CourseProgressDetail{}, fmt.Errorf("upsert reach: %w", err)
	}
	return s.buildDetail(ctx, studentID, expID, courseID)
}

func (s *Service) CompletePage(ctx context.Context, studentID, pageID uuid.UUID) (CourseProgressDetail, error) {
	courseID, err := s.resolvePageCourse(ctx, pageID)
	if err != nil {
		return CourseProgressDetail{}, err
	}
	expID, err := s.authorizeStudentCourse(ctx, studentID, courseID)
	if err != nil {
		return CourseProgressDetail{}, err
	}
	if _, err := s.repo.UpsertComplete(ctx, studentID, pageID, s.now().UTC()); err != nil {
		return CourseProgressDetail{}, fmt.Errorf("upsert complete: %w", err)
	}
	return s.buildDetail(ctx, studentID, expID, courseID)
}

func (s *Service) resolvePageCourse(ctx context.Context, pageID uuid.UUID) (uuid.UUID, error) {
	page, err := s.repo.PageByID(ctx, pageID)
	if err != nil {
		if errors.Is(err, ErrPageNotFound) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, fmt.Errorf("get page: %w", err)
	}
	return page.CourseID, nil
}

// Order matters: a missing current experiment yields NotFound before any
// course-level check can leak into Forbidden.
func (s *Service) authorizeStudentCourse(ctx context.Context, studentID, courseID uuid.UUID) (uuid.UUID, error) {
	expID, found, err := s.experiment.CurrentExperimentForStudent(ctx, studentID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("current experiment for student: %w", err)
	}
	if !found {
		return uuid.Nil, ErrNotFound
	}

	decision, err := s.access.CheckStudentCourseAccess(ctx, studentID, courseID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("check student course access: %w", err)
	}
	if !decision.Found {
		return uuid.Nil, ErrNotFound
	}
	if !decision.Allowed {
		return uuid.Nil, handlerutil.ErrForbidden
	}
	return expID, nil
}

const (
	maxMgmtPageSize = 100
	minMgmtPage     = 1
)

func (s *Service) ListCourseStudentProgress(ctx context.Context, input ListCourseStudentProgressInput) (StudentCourseProgressPage, error) {
	if input.Page < minMgmtPage || input.PageSize < 1 || input.PageSize > maxMgmtPageSize {
		return StudentCourseProgressPage{}, fmt.Errorf("%w: page must be >= 1 and pageSize between 1 and %d", ErrInvalidInput, maxMgmtPageSize)
	}

	exists, err := s.repo.ExperimentCourseExists(ctx, input.ExperimentID, input.CourseID)
	if err != nil {
		return StudentCourseProgressPage{}, fmt.Errorf("check experiment-course assignment: %w", err)
	}
	if !exists {
		return StudentCourseProgressPage{}, ErrNotFound
	}

	total, err := s.repo.CountCourseParticipants(ctx, input.ExperimentID)
	if err != nil {
		return StudentCourseProgressPage{}, fmt.Errorf("count participants: %w", err)
	}

	offset := int64(input.Page-1) * int64(input.PageSize)
	participants, err := s.repo.ListCourseParticipants(ctx, input.ExperimentID, input.PageSize, offset)
	if err != nil {
		return StudentCourseProgressPage{}, fmt.Errorf("list participants: %w", err)
	}

	pages, err := s.repo.PagesByCourse(ctx, input.CourseID)
	if err != nil {
		return StudentCourseProgressPage{}, fmt.Errorf("list pages: %w", err)
	}

	ids := make([]uuid.UUID, 0, len(participants))
	for _, p := range participants {
		ids = append(ids, p.ID)
	}
	progressByStudent, err := s.repo.ProgressForParticipants(ctx, input.CourseID, ids)
	if err != nil {
		return StudentCourseProgressPage{}, fmt.Errorf("fetch participant progress: %w", err)
	}

	items := make([]StudentCourseProgress, 0, len(participants))
	for _, p := range participants {
		summary, _ := summarize(pages, progressByStudent[p.ID])
		items = append(items, StudentCourseProgress{Participant: p, Summary: summary})
	}

	totalPages := int32(0)
	if total > 0 {
		totalPages = int32((total + int64(input.PageSize) - 1) / int64(input.PageSize))
	}
	return StudentCourseProgressPage{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  int32(total),
		CurrentPage: input.Page,
		PageSize:    input.PageSize,
		HasNextPage: input.Page < totalPages,
	}, nil
}

func (s *Service) GetStudentCourseProgress(ctx context.Context, studentID, experimentID, courseID uuid.UUID) (CourseProgressDetail, error) {
	assigned, err := s.repo.ExperimentCourseExists(ctx, experimentID, courseID)
	if err != nil {
		return CourseProgressDetail{}, fmt.Errorf("check experiment-course assignment: %w", err)
	}
	if !assigned {
		return CourseProgressDetail{}, ErrNotFound
	}

	participates, err := s.repo.ParticipantInExperiment(ctx, experimentID, studentID)
	if err != nil {
		return CourseProgressDetail{}, fmt.Errorf("check participant membership: %w", err)
	}
	if !participates {
		return CourseProgressDetail{}, ErrNotFound
	}

	return s.buildDetail(ctx, studentID, experimentID, courseID)
}

func (s *Service) buildDetail(ctx context.Context, studentID, experimentID, courseID uuid.UUID) (CourseProgressDetail, error) {
	pages, err := s.repo.PagesByCourse(ctx, courseID)
	if err != nil {
		return CourseProgressDetail{}, fmt.Errorf("list pages: %w", err)
	}
	rows, err := s.repo.ProgressByStudentCourse(ctx, studentID, courseID)
	if err != nil {
		return CourseProgressDetail{}, fmt.Errorf("list progress: %w", err)
	}
	summary, views := summarize(pages, rows)
	return CourseProgressDetail{
		StudentID:    studentID,
		ExperimentID: experimentID,
		CourseID:     courseID,
		Summary:      summary,
		Pages:        views,
	}, nil
}
