package progress

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Repository interface {
	PageByID(ctx context.Context, pageID uuid.UUID) (PageInfo, error)
	PagesByCourse(ctx context.Context, courseID uuid.UUID) ([]PageInfo, error)
	ProgressByStudentCourse(ctx context.Context, studentID, courseID uuid.UUID) ([]ProgressRow, error)
	UpsertReach(ctx context.Context, studentID, pageID uuid.UUID, reachedAt time.Time) (ProgressRow, error)
	UpsertComplete(ctx context.Context, studentID, pageID uuid.UUID, completedAt time.Time) (ProgressRow, error)
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
	if err := s.authorizeStudentCourse(ctx, studentID, courseID); err != nil {
		return CourseProgressDetail{}, err
	}
	return s.buildDetail(ctx, studentID, courseID)
}

func (s *Service) ReachPage(ctx context.Context, studentID, pageID uuid.UUID) (CourseProgressDetail, error) {
	courseID, err := s.resolvePageCourse(ctx, pageID)
	if err != nil {
		return CourseProgressDetail{}, err
	}
	if err := s.authorizeStudentCourse(ctx, studentID, courseID); err != nil {
		return CourseProgressDetail{}, err
	}
	if _, err := s.repo.UpsertReach(ctx, studentID, pageID, s.now().UTC()); err != nil {
		return CourseProgressDetail{}, fmt.Errorf("upsert reach: %w", err)
	}
	return s.buildDetail(ctx, studentID, courseID)
}

func (s *Service) CompletePage(ctx context.Context, studentID, pageID uuid.UUID) (CourseProgressDetail, error) {
	courseID, err := s.resolvePageCourse(ctx, pageID)
	if err != nil {
		return CourseProgressDetail{}, err
	}
	if err := s.authorizeStudentCourse(ctx, studentID, courseID); err != nil {
		return CourseProgressDetail{}, err
	}
	if _, err := s.repo.UpsertComplete(ctx, studentID, pageID, s.now().UTC()); err != nil {
		return CourseProgressDetail{}, fmt.Errorf("upsert complete: %w", err)
	}
	return s.buildDetail(ctx, studentID, courseID)
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

// Order matters: a missing current experiment yields ErrNotFound before any
// course-level check can leak into ErrForbidden.
func (s *Service) authorizeStudentCourse(ctx context.Context, studentID, courseID uuid.UUID) error {
	_, found, err := s.experiment.CurrentExperimentForStudent(ctx, studentID)
	if err != nil {
		return fmt.Errorf("current experiment for student: %w", err)
	}
	if !found {
		return ErrNotFound
	}

	decision, err := s.access.CheckStudentCourseAccess(ctx, studentID, courseID)
	if err != nil {
		return fmt.Errorf("check student course access: %w", err)
	}
	if !decision.Found {
		return ErrNotFound
	}
	if !decision.Allowed {
		return ErrForbidden
	}
	return nil
}

func (s *Service) buildDetail(ctx context.Context, studentID, courseID uuid.UUID) (CourseProgressDetail, error) {
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
		CourseID: courseID,
		Summary:  summary,
		Pages:    views,
	}, nil
}
