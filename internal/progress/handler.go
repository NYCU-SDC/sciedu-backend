package progress

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"sciedu-backend/internal/auth"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultListPage     int32 = 1
	defaultListPageSize int32 = 20
)

type HandlerService interface {
	GetMyCourseProgress(ctx context.Context, studentID, courseID uuid.UUID) (CourseProgressDetail, error)
	ReachPage(ctx context.Context, studentID, pageID uuid.UUID) (CourseProgressDetail, error)
	CompletePage(ctx context.Context, studentID, pageID uuid.UUID) (CourseProgressDetail, error)
	ListCourseStudentProgress(ctx context.Context, input ListCourseStudentProgressInput) (StudentCourseProgressPage, error)
	GetStudentCourseProgress(ctx context.Context, studentID, experimentID, courseID uuid.UUID) (CourseProgressDetail, error)
}

type Handler struct {
	service       HandlerService
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
}

func NewHandler(service HandlerService, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Handler{
		service: service,
		logger:  logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			switch {
			case errors.Is(err, ErrInvalidInput):
				return problemutil.NewValidateProblem(err.Error())
			case errors.Is(err, ErrNotFound):
				return problemutil.Problem{
					Title:  "Not Found",
					Status: http.StatusNotFound,
					Type:   "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/404",
					Detail: err.Error(),
				}
			}
			return problemutil.Problem{}
		}),
	}
}

// TODO(experiment-scope): add owner / collaborator check using experiments.created_by once the model exists.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set, authorizer *auth.Authorizer) {
	studentAccess := middlewares.Append(authorizer.RequireAnyRole(auth.STUDENT))
	managementAccess := middlewares.Append(authorizer.RequireAnyRole(auth.EXPERIMENTER, auth.ADMIN))
	handle := func(pattern string, set *middlewareutil.Set, fn http.HandlerFunc) {
		mux.HandleFunc(pattern, set.HandlerFunc(fn))
	}

	handle("GET /api/progress/me", studentAccess, h.GetMyCourseProgress)
	handle("PUT /api/progress/me/pages/{pageId}/reach", studentAccess, h.ReachPage)
	handle("PUT /api/progress/me/pages/{pageId}/completion", studentAccess, h.CompletePage)
	handle("GET /api/progress/students", managementAccess, h.ListCourseStudents)
	handle("GET /api/progress/students/{studentId}", managementAccess, h.GetStudentCourseProgress)
}

func (h *Handler) GetMyCourseProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	studentID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	courseID, err := parseRequiredUUID(r.URL.Query().Get("courseId"), "courseId")
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	detail, err := h.service.GetMyCourseProgress(ctx, studentID, courseID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, detailResponse(detail))
}

func (h *Handler) ReachPage(w http.ResponseWriter, r *http.Request) {
	h.markPage(w, r, h.service.ReachPage)
}

func (h *Handler) CompletePage(w http.ResponseWriter, r *http.Request) {
	h.markPage(w, r, h.service.CompletePage)
}

func (h *Handler) markPage(w http.ResponseWriter, r *http.Request, op func(context.Context, uuid.UUID, uuid.UUID) (CourseProgressDetail, error)) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	studentID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}
	pageID, err := handlerutil.ParseUUID(r.PathValue("pageId"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	detail, err := op(ctx, studentID, pageID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, detailResponse(detail))
}

func (h *Handler) ListCourseStudents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	query := r.URL.Query()

	experimentID, err := parseRequiredUUID(query.Get("experimentId"), "experimentId")
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	courseID, err := parseRequiredUUID(query.Get("courseId"), "courseId")
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	page, err := parseIntQuery(query.Get("page"), query.Has("page"), defaultListPage)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	pageSize, err := parseIntQuery(query.Get("pageSize"), query.Has("pageSize"), defaultListPageSize)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	result, err := h.service.ListCourseStudentProgress(ctx, ListCourseStudentProgressInput{
		ExperimentID: experimentID,
		CourseID:     courseID,
		Page:         page,
		PageSize:     pageSize,
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	items := make([]studentCourseProgressResponse, 0, len(result.Items))
	for _, it := range result.Items {
		items = append(items, studentCourseProgressResponse{
			Student:    userResponseFromParticipant(it.Participant),
			AssignedAt: it.Participant.AssignedAt,
			Progress:   summaryResponse(it.Participant.ID, experimentID, courseID, it.Summary),
		})
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, paginatedStudentProgressResponse{
		Items:       items,
		TotalPages:  result.TotalPages,
		TotalItems:  result.TotalItems,
		CurrentPage: result.CurrentPage,
		PageSize:    result.PageSize,
		HasNextPage: result.HasNextPage,
	})
}

func (h *Handler) GetStudentCourseProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	query := r.URL.Query()

	studentID, err := handlerutil.ParseUUID(r.PathValue("studentId"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	experimentID, err := parseRequiredUUID(query.Get("experimentId"), "experimentId")
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	courseID, err := parseRequiredUUID(query.Get("courseId"), "courseId")
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	detail, err := h.service.GetStudentCourseProgress(ctx, studentID, experimentID, courseID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, detailResponse(detail))
}

type pageReferenceResponse struct {
	PageID     uuid.UUID `json:"pageId"`
	PageNumber int32     `json:"pageNumber"`
	Title      string    `json:"title"`
}

type pageProgressResponse struct {
	PageID      uuid.UUID  `json:"pageId"`
	PageNumber  int32      `json:"pageNumber"`
	Title       string     `json:"title"`
	Reached     bool       `json:"reached"`
	ReachedAt   *time.Time `json:"reachedAt,omitempty"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type courseProgressSummaryResponse struct {
	StudentID            uuid.UUID              `json:"studentId"`
	ExperimentID         uuid.UUID              `json:"experimentId"`
	CourseID             uuid.UUID              `json:"courseId"`
	Status               Status                 `json:"status"`
	HighestReachedPage   *pageReferenceResponse `json:"highestReachedPage,omitempty"`
	ReachedPageCount     int32                  `json:"reachedPageCount"`
	CompletedPageCount   int32                  `json:"completedPageCount"`
	TotalPageCount       int32                  `json:"totalPageCount"`
	CompletionPercentage int32                  `json:"completionPercentage"`
}

type courseProgressDetailResponse struct {
	courseProgressSummaryResponse
	Pages []pageProgressResponse `json:"pages"`
}

type userResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL *string   `json:"avatarUrl,omitempty"`
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type studentCourseProgressResponse struct {
	Student    userResponse                  `json:"student"`
	AssignedAt time.Time                     `json:"assignedAt"`
	Progress   courseProgressSummaryResponse `json:"progress"`
}

type paginatedStudentProgressResponse struct {
	Items       []studentCourseProgressResponse `json:"items"`
	TotalPages  int32                           `json:"totalPages"`
	TotalItems  int32                           `json:"totalItems"`
	CurrentPage int32                           `json:"currentPage"`
	PageSize    int32                           `json:"pageSize"`
	HasNextPage bool                            `json:"hasNextPage"`
}

func summaryResponse(studentID, experimentID, courseID uuid.UUID, s CourseProgressSummary) courseProgressSummaryResponse {
	out := courseProgressSummaryResponse{
		StudentID:            studentID,
		ExperimentID:         experimentID,
		CourseID:             courseID,
		Status:               s.Status,
		ReachedPageCount:     s.ReachedPages,
		CompletedPageCount:   s.CompletedPages,
		TotalPageCount:       s.TotalPages,
		CompletionPercentage: s.CompletionPercentage,
	}
	if s.HighestReachedPage != nil {
		out.HighestReachedPage = &pageReferenceResponse{
			PageID:     s.HighestReachedPage.PageID,
			PageNumber: s.HighestReachedPage.PageNumber,
			Title:      s.HighestReachedPage.Title,
		}
	}
	return out
}

func detailResponse(d CourseProgressDetail) courseProgressDetailResponse {
	pages := make([]pageProgressResponse, 0, len(d.Pages))
	for _, p := range d.Pages {
		pages = append(pages, pageProgressResponse{
			PageID:      p.PageID,
			PageNumber:  p.PageNumber,
			Title:       p.Title,
			Reached:     p.ReachedAt != nil,
			ReachedAt:   p.ReachedAt,
			Completed:   p.CompletedAt != nil,
			CompletedAt: p.CompletedAt,
		})
	}
	return courseProgressDetailResponse{
		courseProgressSummaryResponse: summaryResponse(d.StudentID, d.ExperimentID, d.CourseID, d.Summary),
		Pages:                         pages,
	}
}

func userResponseFromParticipant(p Participant) userResponse {
	return userResponse{
		ID:        p.ID,
		Email:     p.Email,
		Name:      p.Name,
		AvatarURL: p.AvatarURL,
		Roles:     p.Roles,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func parseRequiredUUID(raw, name string) (uuid.UUID, error) {
	value, err := uuid.Parse(raw)
	if err != nil || value == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: %s must be a UUID", ErrInvalidInput, name)
	}
	return value, nil
}

func parseIntQuery(raw string, present bool, fallback int32) (int32, error) {
	if !present {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: integer query parameter is malformed", ErrInvalidInput)
	}
	return int32(value), nil
}
