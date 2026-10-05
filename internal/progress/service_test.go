package progress

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRepo struct {
	pageByID            func(uuid.UUID) (PageInfo, error)
	pagesByCourse       func(uuid.UUID) ([]PageInfo, error)
	progressByStudent   func(uuid.UUID, uuid.UUID) ([]ProgressRow, error)
	upsertReach         func(uuid.UUID, uuid.UUID, time.Time) (ProgressRow, error)
	upsertComplete      func(uuid.UUID, uuid.UUID, time.Time) (ProgressRow, error)
	listParticipants    func(uuid.UUID, int32, int64) ([]Participant, error)
	countParticipants   func(uuid.UUID) (int64, error)
	progressForParts    func(uuid.UUID, []uuid.UUID) (map[uuid.UUID][]ProgressRow, error)
	expCourseExists     func(uuid.UUID, uuid.UUID) (bool, error)
	participantIn       func(uuid.UUID, uuid.UUID) (bool, error)
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
func (s *stubRepo) ListCourseParticipants(_ context.Context, eid uuid.UUID, l int32, o int64) ([]Participant, error) {
	return s.listParticipants(eid, l, o)
}
func (s *stubRepo) CountCourseParticipants(_ context.Context, eid uuid.UUID) (int64, error) {
	return s.countParticipants(eid)
}
func (s *stubRepo) ProgressForParticipants(_ context.Context, cid uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]ProgressRow, error) {
	return s.progressForParts(cid, ids)
}
func (s *stubRepo) ExperimentCourseExists(_ context.Context, eid, cid uuid.UUID) (bool, error) {
	return s.expCourseExists(eid, cid)
}
func (s *stubRepo) ParticipantInExperiment(_ context.Context, eid, sid uuid.UUID) (bool, error) {
	return s.participantIn(eid, sid)
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

	t.Run("no current experiment returns NotFound", func(t *testing.T) {
		svc := newServiceWithStubs(newHappyRepo(),
			stubExperiment{found: false},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)
		_, err := svc.GetMyCourseProgress(context.Background(), testStudentID, testCourseID)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("course not found returns NotFound", func(t *testing.T) {
		svc := newServiceWithStubs(newHappyRepo(),
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: false}},
			testNow)
		_, err := svc.GetMyCourseProgress(context.Background(), testStudentID, testCourseID)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("course exists but unallowed returns Forbidden", func(t *testing.T) {
		svc := newServiceWithStubs(newHappyRepo(),
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: false}},
			testNow)
		_, err := svc.GetMyCourseProgress(context.Background(), testStudentID, testCourseID)
		assert.ErrorIs(t, err, handlerutil.ErrForbidden)
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

	t.Run("missing page returns NotFound", func(t *testing.T) {
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
		assert.ErrorIs(t, err, handlerutil.ErrForbidden)
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

	t.Run("missing page returns NotFound", func(t *testing.T) {
		repo := newHappyRepo()
		svc := newServiceWithStubs(repo,
			stubExperiment{id: testExpID, found: true},
			stubAccess{decision: CourseAccessDecision{Found: true, Allowed: true}},
			testNow)

		_, err := svc.CompletePage(context.Background(), testStudentID, uuid.New())
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Equal(t, 0, repo.upsertCompleteCalls)
	})

	t.Run("no current experiment returns NotFound without upserting", func(t *testing.T) {
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

func newMgmtRepo(participants []Participant, pages []PageInfo, progress map[uuid.UUID][]ProgressRow) *stubRepo {
	return &stubRepo{
		pagesByCourse: func(_ uuid.UUID) ([]PageInfo, error) { return pages, nil },
		progressByStudent: func(sid, _ uuid.UUID) ([]ProgressRow, error) {
			return progress[sid], nil
		},
		listParticipants: func(_ uuid.UUID, limit int32, offset int64) ([]Participant, error) {
			start := int(offset)
			if start >= len(participants) {
				return nil, nil
			}
			end := start + int(limit)
			if end > len(participants) {
				end = len(participants)
			}
			return participants[start:end], nil
		},
		countParticipants: func(_ uuid.UUID) (int64, error) {
			return int64(len(participants)), nil
		},
		progressForParts: func(_ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]ProgressRow, error) {
			out := make(map[uuid.UUID][]ProgressRow, len(ids))
			for _, id := range ids {
				if rows, ok := progress[id]; ok {
					out[id] = rows
				}
			}
			return out, nil
		},
		expCourseExists: func(_, _ uuid.UUID) (bool, error) { return true, nil },
		participantIn:   func(_, _ uuid.UUID) (bool, error) { return true, nil },
	}
}

func TestServiceListCourseStudentProgress(t *testing.T) {
	page1 := PageInfo{ID: uuid.New(), Title: "P1", DisplayOrder: 0}
	page2 := PageInfo{ID: uuid.New(), Title: "P2", DisplayOrder: 1}

	alice := Participant{ID: uuid.New(), Email: "a@x", Name: "Alice"}
	bob := Participant{ID: uuid.New(), Email: "b@x", Name: "Bob"}
	carol := Participant{ID: uuid.New(), Email: "c@x", Name: "Carol"}

	t.Run("happy path returns per-student summaries", func(t *testing.T) {
		completedAt := testNow
		progress := map[uuid.UUID][]ProgressRow{
			alice.ID: {{PageID: page1.ID, ReachedAt: testNow, CompletedAt: &completedAt}},
			// bob has no progress — must still appear
		}
		repo := newMgmtRepo([]Participant{alice, bob}, []PageInfo{page1, page2}, progress)

		pageResult, err := svcMgmt(repo).ListCourseStudentProgress(context.Background(), ListCourseStudentProgressInput{
			ExperimentID: testExpID, CourseID: testCourseID, Page: 1, PageSize: 20,
		})
		require.NoError(t, err)
		require.Len(t, pageResult.Items, 2)
		assert.Equal(t, alice.ID, pageResult.Items[0].Participant.ID)
		assert.Equal(t, StatusInProgress, pageResult.Items[0].Summary.Status)
		assert.Equal(t, int32(1), pageResult.Items[0].Summary.CompletedPages)
		assert.Equal(t, bob.ID, pageResult.Items[1].Participant.ID)
		assert.Equal(t, StatusNotStarted, pageResult.Items[1].Summary.Status)
		assert.Equal(t, int32(2), pageResult.TotalItems)
		assert.Equal(t, int32(1), pageResult.TotalPages)
		assert.False(t, pageResult.HasNextPage)
	})

	t.Run("pagination slices and sets HasNextPage", func(t *testing.T) {
		repo := newMgmtRepo([]Participant{alice, bob, carol}, []PageInfo{page1}, nil)

		pageResult, err := svcMgmt(repo).ListCourseStudentProgress(context.Background(), ListCourseStudentProgressInput{
			ExperimentID: testExpID, CourseID: testCourseID, Page: 1, PageSize: 2,
		})
		require.NoError(t, err)
		require.Len(t, pageResult.Items, 2)
		assert.Equal(t, int32(3), pageResult.TotalItems)
		assert.Equal(t, int32(2), pageResult.TotalPages)
		assert.True(t, pageResult.HasNextPage)

		pageResult, err = svcMgmt(repo).ListCourseStudentProgress(context.Background(), ListCourseStudentProgressInput{
			ExperimentID: testExpID, CourseID: testCourseID, Page: 2, PageSize: 2,
		})
		require.NoError(t, err)
		require.Len(t, pageResult.Items, 1)
		assert.False(t, pageResult.HasNextPage)
	})

	t.Run("course not assigned returns NotFound", func(t *testing.T) {
		repo := newMgmtRepo(nil, nil, nil)
		repo.expCourseExists = func(_, _ uuid.UUID) (bool, error) { return false, nil }

		_, err := svcMgmt(repo).ListCourseStudentProgress(context.Background(), ListCourseStudentProgressInput{
			ExperimentID: testExpID, CourseID: testCourseID, Page: 1, PageSize: 20,
		})
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("invalid pagination returns ErrInvalidInput", func(t *testing.T) {
		repo := newMgmtRepo(nil, nil, nil)
		for _, in := range []ListCourseStudentProgressInput{
			{Page: 0, PageSize: 20},
			{Page: 1, PageSize: 0},
			{Page: 1, PageSize: 101},
		} {
			in.ExperimentID = testExpID
			in.CourseID = testCourseID
			_, err := svcMgmt(repo).ListCourseStudentProgress(context.Background(), in)
			assert.ErrorIs(t, err, ErrInvalidInput)
		}
	})
}

func TestServiceGetStudentCourseProgress(t *testing.T) {
	page1 := PageInfo{ID: uuid.New(), Title: "P1", DisplayOrder: 0}
	completedAt := testNow
	progress := map[uuid.UUID][]ProgressRow{
		testStudentID: {{PageID: page1.ID, ReachedAt: testNow, CompletedAt: &completedAt}},
	}

	newRepo := func() *stubRepo {
		return newMgmtRepo([]Participant{{ID: testStudentID}}, []PageInfo{page1}, progress)
	}

	t.Run("happy path returns detail", func(t *testing.T) {
		detail, err := svcMgmt(newRepo()).GetStudentCourseProgress(context.Background(), testStudentID, testExpID, testCourseID)
		require.NoError(t, err)
		assert.Equal(t, testCourseID, detail.CourseID)
		assert.Equal(t, StatusCompleted, detail.Summary.Status)
	})

	t.Run("course not assigned returns NotFound", func(t *testing.T) {
		repo := newRepo()
		repo.expCourseExists = func(_, _ uuid.UUID) (bool, error) { return false, nil }
		_, err := svcMgmt(repo).GetStudentCourseProgress(context.Background(), testStudentID, testExpID, testCourseID)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("student not participant returns NotFound", func(t *testing.T) {
		repo := newRepo()
		repo.participantIn = func(_, _ uuid.UUID) (bool, error) { return false, nil }
		_, err := svcMgmt(repo).GetStudentCourseProgress(context.Background(), testStudentID, testExpID, testCourseID)
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func svcMgmt(repo *stubRepo) *Service {
	return NewService(repo, stubExperiment{}, stubAccess{}, fixedClock(testNow), nil)
}
