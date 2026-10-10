package progress

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sciedu-backend/internal/auth"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubHandlerService struct {
	getMy       func(ctx context.Context, studentID, courseID uuid.UUID) (CourseProgressDetail, error)
	reach       func(ctx context.Context, studentID, pageID uuid.UUID) (CourseProgressDetail, error)
	complete    func(ctx context.Context, studentID, pageID uuid.UUID) (CourseProgressDetail, error)
	listStudent func(ctx context.Context, input ListCourseStudentProgressInput) (StudentCourseProgressPage, error)
	getStudent  func(ctx context.Context, studentID, experimentID, courseID uuid.UUID) (CourseProgressDetail, error)
}

func (s *stubHandlerService) GetMyCourseProgress(ctx context.Context, sid, cid uuid.UUID) (CourseProgressDetail, error) {
	return s.getMy(ctx, sid, cid)
}
func (s *stubHandlerService) ReachPage(ctx context.Context, sid, pid uuid.UUID) (CourseProgressDetail, error) {
	return s.reach(ctx, sid, pid)
}
func (s *stubHandlerService) CompletePage(ctx context.Context, sid, pid uuid.UUID) (CourseProgressDetail, error) {
	return s.complete(ctx, sid, pid)
}
func (s *stubHandlerService) ListCourseStudentProgress(ctx context.Context, in ListCourseStudentProgressInput) (StudentCourseProgressPage, error) {
	return s.listStudent(ctx, in)
}
func (s *stubHandlerService) GetStudentCourseProgress(ctx context.Context, sid, eid, cid uuid.UUID) (CourseProgressDetail, error) {
	return s.getStudent(ctx, sid, eid, cid)
}

func withStudent(r *http.Request, studentID uuid.UUID) *http.Request {
	return r.WithContext(auth.ContextWithUserID(r.Context(), studentID))
}

func makeHandler(svc HandlerService) *Handler {
	return NewHandler(svc, nil)
}

func sampleDetail(studentID, courseID uuid.UUID) CourseProgressDetail {
	expID := uuid.New()
	pageID := uuid.New()
	reached := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	return CourseProgressDetail{
		StudentID:    studentID,
		ExperimentID: expID,
		CourseID:     courseID,
		Summary: CourseProgressSummary{
			TotalPages:           1,
			ReachedPages:         1,
			CompletedPages:       0,
			CompletionPercentage: 0,
			Status:               StatusInProgress,
			HighestReachedPage: &ProgressPageReference{
				PageID:     pageID,
				PageNumber: 1,
				Title:      "P1",
			},
		},
		Pages: []PageProgressView{
			{PageID: pageID, PageNumber: 1, Title: "P1", ReachedAt: &reached},
		},
	}
}

func TestHandlerGetMyCourseProgress(t *testing.T) {
	studentID := uuid.New()
	courseID := uuid.New()

	t.Run("200 happy path", func(t *testing.T) {
		detail := sampleDetail(studentID, courseID)
		svc := &stubHandlerService{
			getMy: func(_ context.Context, sid, cid uuid.UUID) (CourseProgressDetail, error) {
				assert.Equal(t, studentID, sid)
				assert.Equal(t, courseID, cid)
				return detail, nil
			},
		}
		req := withStudent(httptest.NewRequest(http.MethodGet, "/api/progress/me?courseId="+courseID.String(), nil), studentID)
		rr := httptest.NewRecorder()
		makeHandler(svc).GetMyCourseProgress(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		var body courseProgressDetailResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
		assert.Equal(t, studentID, body.StudentID)
		assert.Equal(t, courseID, body.CourseID)
		assert.Equal(t, StatusInProgress, body.Status)
		require.NotNil(t, body.HighestReachedPage)
		assert.Equal(t, int32(1), body.HighestReachedPage.PageNumber)
		require.Len(t, body.Pages, 1)
		assert.True(t, body.Pages[0].Reached)
		assert.False(t, body.Pages[0].Completed)
	})

	t.Run("401 without session", func(t *testing.T) {
		svc := &stubHandlerService{}
		req := httptest.NewRequest(http.MethodGet, "/api/progress/me?courseId="+courseID.String(), nil)
		rr := httptest.NewRecorder()
		makeHandler(svc).GetMyCourseProgress(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("400 missing courseId", func(t *testing.T) {
		svc := &stubHandlerService{}
		req := withStudent(httptest.NewRequest(http.MethodGet, "/api/progress/me", nil), studentID)
		rr := httptest.NewRecorder()
		makeHandler(svc).GetMyCourseProgress(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("404 service returns NotFound", func(t *testing.T) {
		svc := &stubHandlerService{
			getMy: func(_ context.Context, _, _ uuid.UUID) (CourseProgressDetail, error) {
				return CourseProgressDetail{}, ErrNotFound
			},
		}
		req := withStudent(httptest.NewRequest(http.MethodGet, "/api/progress/me?courseId="+courseID.String(), nil), studentID)
		rr := httptest.NewRecorder()
		makeHandler(svc).GetMyCourseProgress(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("403 service returns Forbidden", func(t *testing.T) {
		svc := &stubHandlerService{
			getMy: func(_ context.Context, _, _ uuid.UUID) (CourseProgressDetail, error) {
				return CourseProgressDetail{}, handlerutil.ErrForbidden
			},
		}
		req := withStudent(httptest.NewRequest(http.MethodGet, "/api/progress/me?courseId="+courseID.String(), nil), studentID)
		rr := httptest.NewRecorder()
		makeHandler(svc).GetMyCourseProgress(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})
}

func TestHandlerReachPage(t *testing.T) {
	studentID := uuid.New()
	pageID := uuid.New()

	t.Run("200 happy path forwards pageId", func(t *testing.T) {
		svc := &stubHandlerService{
			reach: func(_ context.Context, sid, pid uuid.UUID) (CourseProgressDetail, error) {
				assert.Equal(t, studentID, sid)
				assert.Equal(t, pageID, pid)
				return sampleDetail(sid, uuid.New()), nil
			},
		}
		req := withStudent(httptest.NewRequest(http.MethodPut, "/api/progress/me/pages/"+pageID.String()+"/reach", nil), studentID)
		req.SetPathValue("pageId", pageID.String())
		rr := httptest.NewRecorder()
		makeHandler(svc).ReachPage(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("400 malformed pageId", func(t *testing.T) {
		svc := &stubHandlerService{}
		req := withStudent(httptest.NewRequest(http.MethodPut, "/api/progress/me/pages/not-a-uuid/reach", nil), studentID)
		req.SetPathValue("pageId", "not-a-uuid")
		rr := httptest.NewRecorder()
		makeHandler(svc).ReachPage(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestHandlerListCourseStudents(t *testing.T) {
	experimentID := uuid.New()
	courseID := uuid.New()
	studentID := uuid.New()

	t.Run("200 happy path", func(t *testing.T) {
		svc := &stubHandlerService{
			listStudent: func(_ context.Context, in ListCourseStudentProgressInput) (StudentCourseProgressPage, error) {
				assert.Equal(t, experimentID, in.ExperimentID)
				assert.Equal(t, courseID, in.CourseID)
				assert.Equal(t, int32(1), in.Page)
				assert.Equal(t, int32(20), in.PageSize)
				return StudentCourseProgressPage{
					Items: []StudentCourseProgress{{
						Participant: Participant{
							ID: studentID, Email: "a@x", Name: "A", Roles: []string{"STUDENT"},
							AssignedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
							CreatedAt:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
							UpdatedAt:  time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
						},
						Summary: CourseProgressSummary{TotalPages: 1, Status: StatusNotStarted},
					}},
					TotalPages: 1, TotalItems: 1, CurrentPage: 1, PageSize: 20, HasNextPage: false,
				}, nil
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/progress/students?experimentId="+experimentID.String()+"&courseId="+courseID.String(), nil)
		rr := httptest.NewRecorder()
		makeHandler(svc).ListCourseStudents(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		var body paginatedStudentProgressResponse
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
		require.Len(t, body.Items, 1)
		assert.Equal(t, studentID, body.Items[0].Student.ID)
		assert.Equal(t, studentID, body.Items[0].Progress.StudentID)
		assert.Equal(t, experimentID, body.Items[0].Progress.ExperimentID)
	})

	t.Run("400 missing experimentId", func(t *testing.T) {
		svc := &stubHandlerService{}
		req := httptest.NewRequest(http.MethodGet, "/api/progress/students?courseId="+courseID.String(), nil)
		rr := httptest.NewRecorder()
		makeHandler(svc).ListCourseStudents(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("404 service returns NotFound", func(t *testing.T) {
		svc := &stubHandlerService{
			listStudent: func(_ context.Context, _ ListCourseStudentProgressInput) (StudentCourseProgressPage, error) {
				return StudentCourseProgressPage{}, ErrNotFound
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/progress/students?experimentId="+experimentID.String()+"&courseId="+courseID.String(), nil)
		rr := httptest.NewRecorder()
		makeHandler(svc).ListCourseStudents(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})

	t.Run("400 invalid pageSize", func(t *testing.T) {
		svc := &stubHandlerService{
			listStudent: func(_ context.Context, _ ListCourseStudentProgressInput) (StudentCourseProgressPage, error) {
				return StudentCourseProgressPage{}, ErrInvalidInput
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/progress/students?experimentId="+experimentID.String()+"&courseId="+courseID.String()+"&pageSize=200", nil)
		rr := httptest.NewRecorder()
		makeHandler(svc).ListCourseStudents(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestHandlerGetStudentCourseProgress(t *testing.T) {
	studentID := uuid.New()
	experimentID := uuid.New()
	courseID := uuid.New()

	t.Run("200 happy path", func(t *testing.T) {
		svc := &stubHandlerService{
			getStudent: func(_ context.Context, sid, eid, cid uuid.UUID) (CourseProgressDetail, error) {
				assert.Equal(t, studentID, sid)
				assert.Equal(t, experimentID, eid)
				assert.Equal(t, courseID, cid)
				return sampleDetail(sid, cid), nil
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/progress/students/"+studentID.String()+"?experimentId="+experimentID.String()+"&courseId="+courseID.String(), nil)
		req.SetPathValue("studentId", studentID.String())
		rr := httptest.NewRecorder()
		makeHandler(svc).GetStudentCourseProgress(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("404 service returns NotFound", func(t *testing.T) {
		svc := &stubHandlerService{
			getStudent: func(_ context.Context, _, _, _ uuid.UUID) (CourseProgressDetail, error) {
				return CourseProgressDetail{}, ErrNotFound
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/api/progress/students/"+studentID.String()+"?experimentId="+experimentID.String()+"&courseId="+courseID.String(), nil)
		req.SetPathValue("studentId", studentID.String())
		rr := httptest.NewRecorder()
		makeHandler(svc).GetStudentCourseProgress(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}
