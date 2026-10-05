package progress

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRepo struct {
	pageByID            func(uuid.UUID) (PageInfo, error)
	pagesByCourse       func(uuid.UUID) ([]PageInfo, error)
	progressByStudent   func(uuid.UUID, uuid.UUID) ([]ProgressRow, error)
	upsertReach         func(uuid.UUID, uuid.UUID, time.Time) (ProgressRow, error)
	upsertComplete      func(uuid.UUID, uuid.UUID, time.Time) (ProgressRow, error)
	upsertReachCalls    int
	upsertCompleteCalls int
	lastReachTime       time.Time
	lastCompleteTime    time.Time
}

func (s *stubRepo) PageByID(_ context.Context, id uuid.UUID) (PageInfo, error) {
	return s.pageByID(id)
}
func (s *stubRepo) PagesByCourse(_ context.Context, id uuid.UUID) ([]PageInfo, error) {
	return s.pagesByCourse(id)
}
func (s *stubRepo) ProgressByStudentCourse(_ context.Context, sid, cid uuid.UUID) ([]ProgressRow, error) {
	return s.progressByStudent(sid, cid)
}
func (s *stubRepo) UpsertReach(_ context.Context, sid, pid uuid.UUID, t time.Time) (ProgressRow, error) {
	s.upsertReachCalls++
	s.lastReachTime = t
	if s.upsertReach == nil {
		return ProgressRow{StudentID: sid, PageID: pid, ReachedAt: t}, nil
	}
	return s.upsertReach(sid, pid, t)
}
func (s *stubRepo) UpsertComplete(_ context.Context, sid, pid uuid.UUID, t time.Time) (ProgressRow, error) {
	s.upsertCompleteCalls++
	s.lastCompleteTime = t
	if s.upsertComplete == nil {
		c := t
		return ProgressRow{StudentID: sid, PageID: pid, ReachedAt: t, CompletedAt: &c}, nil
	}
	return s.upsertComplete(sid, pid, t)
}

type stubExperiment struct {
	id    uuid.UUID
	found bool
	err   error
}

func (s stubExperiment) CurrentExperimentForStudent(_ context.Context, _ uuid.UUID) (uuid.UUID, bool, error) {
	return s.id, s.found, s.err
}

type stubAccess struct {
	decision CourseAccessDecision
	err      error
}

func (s stubAccess) CheckStudentCourseAccess(_ context.Context, _, _ uuid.UUID) (CourseAccessDecision, error) {
	return s.decision, s.err
}

func fixedClock(t time.Time) Clock {
	return func() time.Time { return t }
}

func newServiceWithStubs(repo *stubRepo, exp stubExperiment, acc stubAccess, now time.Time) *Service {
	return NewService(repo, exp, acc, fixedClock(now), nil)
}

var (
	testStudentID = uuid.New()
	testCourseID  = uuid.New()
	testPageID    = uuid.New()
	testExpID     = uuid.New()
	testNow       = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
)

func newHappyRepo() *stubRepo {
	pages := []PageInfo{{ID: testPageID, CourseID: testCourseID, Title: "P1", DisplayOrder: 0}}
	return &stubRepo{
		pageByID: func(id uuid.UUID) (PageInfo, error) {
			if id != testPageID {
				return PageInfo{}, ErrPageNotFound
			}
			return pages[0], nil
		},
		pagesByCourse: func(id uuid.UUID) ([]PageInfo, error) {
			if id != testCourseID {
				return nil, nil
			}
			return pages, nil
		},
		progressByStudent: func(_, _ uuid.UUID) ([]ProgressRow, error) {
			return nil, nil
		},
	}
}

func TestServiceGetMyCourseProgress(t *testing.T) {
	t.Run("happy path returns detail", func(t *testing.T) {
		repo := newHappyRepo()
		svc := newServiceWithStubs(repo,
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)

		detail, err := svc.GetMyCourseProgress(context.Background(), testStudentID, testCourseID)
		require.NoError(t, err)
		assert.Equal(t, testCourseID, detail.CourseID)
		assert.Equal(t, int32(1), detail.Summary.TotalPages)
		assert.Equal(t, StatusNotStarted, detail.Summary.Status)
	})

	t.Run("no current experiment returns ErrNotFound", func(t *testing.T) {
		svc := newServiceWithStubs(newHappyRepo(),
			stubExperiment{found: false},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)
		_, err := svc.GetMyCourseProgress(context.Background(), testStudentID, testCourseID)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("course not found returns ErrNotFound", func(t *testing.T) {
		svc := newServiceWithStubs(newHappyRepo(),
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: false}},
			testNow)
		_, err := svc.GetMyCourseProgress(context.Background(), testStudentID, testCourseID)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("course exists but unallowed returns ErrForbidden", func(t *testing.T) {
		svc := newServiceWithStubs(newHappyRepo(),
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: false}},
			testNow)
		_, err := svc.GetMyCourseProgress(context.Background(), testStudentID, testCourseID)
		assert.ErrorIs(t, err, ErrForbidden)
	})

	t.Run("experiment query error propagates", func(t *testing.T) {
		boom := errors.New("boom")
		svc := newServiceWithStubs(newHappyRepo(),
			stubExperiment{err: boom},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)
		_, err := svc.GetMyCourseProgress(context.Background(), testStudentID, testCourseID)
		assert.ErrorIs(t, err, boom)
	})
}

func TestServiceReachPage(t *testing.T) {
	t.Run("happy path upserts with clock timestamp", func(t *testing.T) {
		repo := newHappyRepo()
		svc := newServiceWithStubs(repo,
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)

		detail, err := svc.ReachPage(context.Background(), testStudentID, testPageID)
		require.NoError(t, err)
		assert.Equal(t, 1, repo.upsertReachCalls)
		assert.True(t, testNow.Equal(repo.lastReachTime))
		assert.Equal(t, testCourseID, detail.CourseID)
	})

	t.Run("missing page returns ErrNotFound", func(t *testing.T) {
		repo := newHappyRepo()
		svc := newServiceWithStubs(repo,
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)

		_, err := svc.ReachPage(context.Background(), testStudentID, uuid.New())
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Equal(t, 0, repo.upsertReachCalls, "upsert must not run when page is missing")
	})

	t.Run("unauthorized student does not call upsert", func(t *testing.T) {
		repo := newHappyRepo()
		svc := newServiceWithStubs(repo,
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: false}},
			testNow)

		_, err := svc.ReachPage(context.Background(), testStudentID, testPageID)
		assert.ErrorIs(t, err, ErrForbidden)
		assert.Equal(t, 0, repo.upsertReachCalls)
	})
}

func TestServiceCompletePage(t *testing.T) {
	t.Run("happy path upserts with clock timestamp", func(t *testing.T) {
		repo := newHappyRepo()
		svc := newServiceWithStubs(repo,
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)

		_, err := svc.CompletePage(context.Background(), testStudentID, testPageID)
		require.NoError(t, err)
		assert.Equal(t, 1, repo.upsertCompleteCalls)
		assert.True(t, testNow.Equal(repo.lastCompleteTime))
	})

	t.Run("missing page returns ErrNotFound", func(t *testing.T) {
		repo := newHappyRepo()
		svc := newServiceWithStubs(repo,
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)

		_, err := svc.CompletePage(context.Background(), testStudentID, uuid.New())
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Equal(t, 0, repo.upsertCompleteCalls)
	})

	t.Run("no current experiment returns ErrNotFound without upserting", func(t *testing.T) {
		repo := newHappyRepo()
		svc := newServiceWithStubs(repo,
			stubExperiment{found: false},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)

		_, err := svc.CompletePage(context.Background(), testStudentID, testPageID)
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Equal(t, 0, repo.upsertCompleteCalls)
	})
}
