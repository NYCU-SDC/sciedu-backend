package pagevisit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	defaultPage     int32 = 1
	defaultPageSize int32 = 20
	maxPageSize     int32 = 100
)

type HandlerService interface {
	Enter(ctx context.Context, input EnterInput) (PageVisit, error)
	Leave(ctx context.Context, input LeaveInput) (PageVisit, error)
	List(ctx context.Context, input ListInput) (VisitPage, error)
}

type Handler struct {
	service       HandlerService
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
}

type createPageVisitRequest struct {
	CourseID        uuid.UUID `json:"courseId"`
	PageID          uuid.UUID `json:"pageId"`
	ClientSessionID uuid.UUID `json:"clientSessionId"`
}

type pageVisitResponse struct {
	ID              uuid.UUID  `json:"id"`
	StudentID       uuid.UUID  `json:"studentId"`
	CourseID        uuid.UUID  `json:"courseId"`
	PageID          uuid.UUID  `json:"pageId"`
	ClientSessionID uuid.UUID  `json:"clientSessionId"`
	EnteredAt       time.Time  `json:"enteredAt"`
	LeftAt          *time.Time `json:"leftAt,omitempty"`
}

type paginatedPageVisitsResponse struct {
	Items       []pageVisitResponse `json:"items"`
	TotalPages  int32               `json:"totalPages"`
	TotalItems  int32               `json:"totalItems"`
	CurrentPage int32               `json:"currentPage"`
	PageSize    int32               `json:"pageSize"`
	HasNextPage bool                `json:"hasNextPage"`
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
					Detail: "page visit not found",
				}
			case errors.Is(err, ErrIdempotencyConflict):
				return problemutil.Problem{
					Title:  "Conflict",
					Status: http.StatusConflict,
					Type:   "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/409",
					Detail: err.Error(),
				}
			default:
				return problemutil.Problem{}
			}
		}),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set, authorizer *auth.Authorizer) {
	studentAccess := middlewares.Append(authorizer.RequireAnyRole(auth.STUDENT))
	managementAccess := middlewares.Append(authorizer.RequireAnyRole(auth.EXPERIMENTER, auth.ADMIN))
	handle := func(pattern string, set *middlewareutil.Set, fn http.HandlerFunc) {
		if set != nil {
			fn = set.HandlerFunc(fn)
		}
		mux.HandleFunc(pattern, fn)
	}

	handle("POST /api/page-visits", studentAccess, h.Enter)
	handle("POST /api/page-visits/{id}/leave", studentAccess, h.Leave)
	handle("GET /api/page-visits", managementAccess, h.List)
}

func (h *Handler) Enter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	studentID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	idempotencyKey, err := parseRequiredUUID(r.Header.Get("Idempotency-Key"), "Idempotency-Key")
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	request, err := parseCreatePageVisitRequest(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	visit, err := h.service.Enter(ctx, EnterInput{
		StudentID:       studentID,
		CourseID:        request.CourseID,
		PageID:          request.PageID,
		ClientSessionID: request.ClientSessionID,
		IdempotencyKey:  idempotencyKey,
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusCreated, responseFromPageVisit(visit))
}

func (h *Handler) Leave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	studentID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}
	visitID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	visit, err := h.service.Leave(ctx, LeaveInput{StudentID: studentID, VisitID: visitID})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, responseFromPageVisit(visit))
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	input, err := parseListInput(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	page, err := h.service.List(ctx, input)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	items := make([]pageVisitResponse, 0, len(page.Items))
	for _, visit := range page.Items {
		items = append(items, responseFromPageVisit(visit))
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, paginatedPageVisitsResponse{
		Items:       items,
		TotalPages:  page.TotalPages,
		TotalItems:  page.TotalItems,
		CurrentPage: page.CurrentPage,
		PageSize:    page.PageSize,
		HasNextPage: page.HasNextPage,
	})
}

func parseCreatePageVisitRequest(r *http.Request) (createPageVisitRequest, error) {
	var request createPageVisitRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fmt.Errorf("%w: invalid JSON request body", ErrInvalidInput)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return request, fmt.Errorf("%w: request body must contain one JSON object", ErrInvalidInput)
	}
	if request.CourseID == uuid.Nil || request.PageID == uuid.Nil || request.ClientSessionID == uuid.Nil {
		return request, fmt.Errorf("%w: courseId, pageId, and clientSessionId are required UUIDs", ErrInvalidInput)
	}
	return request, nil
}

func parseListInput(r *http.Request) (ListInput, error) {
	query := r.URL.Query()
	page, err := parseIntQuery(query.Get("page"), query.Has("page"), defaultPage)
	if err != nil || page < 1 {
		return ListInput{}, fmt.Errorf("%w: page must be a positive integer", ErrInvalidInput)
	}
	pageSize, err := parseIntQuery(query.Get("pageSize"), query.Has("pageSize"), defaultPageSize)
	if err != nil || pageSize < 1 || pageSize > maxPageSize {
		return ListInput{}, fmt.Errorf("%w: pageSize must be between 1 and %d", ErrInvalidInput, maxPageSize)
	}

	studentID, err := parseOptionalUUID(query, "studentId")
	if err != nil {
		return ListInput{}, err
	}
	courseID, err := parseOptionalUUID(query, "courseId")
	if err != nil {
		return ListInput{}, err
	}
	pageID, err := parseOptionalUUID(query, "pageId")
	if err != nil {
		return ListInput{}, err
	}
	clientSessionID, err := parseOptionalUUID(query, "clientSessionId")
	if err != nil {
		return ListInput{}, err
	}

	var status *Status
	if query.Has("status") {
		value := Status(query.Get("status"))
		if value != StatusOpen && value != StatusClosed {
			return ListInput{}, fmt.Errorf("%w: status must be OPEN or CLOSED", ErrInvalidInput)
		}
		status = &value
	}
	enteredFrom, err := parseOptionalTime(query, "enteredFrom")
	if err != nil {
		return ListInput{}, err
	}
	enteredBefore, err := parseOptionalTime(query, "enteredBefore")
	if err != nil {
		return ListInput{}, err
	}

	return ListInput{
		StudentID:       studentID,
		CourseID:        courseID,
		PageID:          pageID,
		ClientSessionID: clientSessionID,
		Status:          status,
		EnteredFrom:     enteredFrom,
		EnteredBefore:   enteredBefore,
		Page:            page,
		PageSize:        pageSize,
	}, nil
}

type queryValues interface {
	Get(string) string
	Has(string) bool
}

func parseOptionalUUID(query queryValues, name string) (*uuid.UUID, error) {
	if !query.Has(name) {
		return nil, nil
	}
	value, err := parseRequiredUUID(query.Get(name), name)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func parseRequiredUUID(raw, name string) (uuid.UUID, error) {
	value, err := uuid.Parse(raw)
	if err != nil || value == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: %s must be a UUID", ErrInvalidInput, name)
	}
	return value, nil
}

func parseOptionalTime(query queryValues, name string) (*time.Time, error) {
	if !query.Has(name) {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, query.Get(name))
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be an RFC3339 timestamp", ErrInvalidInput, name)
	}
	return &value, nil
}

func parseIntQuery(raw string, present bool, fallback int32) (int32, error) {
	if !present {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(value), nil
}

func responseFromPageVisit(visit PageVisit) pageVisitResponse {
	response := pageVisitResponse{
		ID:              visit.ID,
		StudentID:       visit.StudentID,
		CourseID:        visit.CourseID,
		PageID:          visit.PageID,
		ClientSessionID: visit.ClientSessionID,
		EnteredAt:       visit.EnteredAt.Time,
	}
	if visit.LeftAt.Valid {
		leftAt := visit.LeftAt.Time
		response.LeftAt = &leftAt
	}
	return response
}
