package course

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sciedu-backend/internal/auth"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultPage           int32 = 1
	defaultPageSize       int32 = 20
	maxPageSize           int32 = 100
	maxCourseSearchLength       = 200
)

type Handler struct {
	service       *Service
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
	validator     *validator.Validate
}

type courseResponse struct {
	ID          uuid.UUID    `json:"id"`
	Code        string       `json:"code"`
	Title       string       `json:"title"`
	Description *string      `json:"description,omitempty"`
	Status      CourseStatus `json:"status"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

type paginatedCoursesResponse struct {
	Items       []courseResponse `json:"items"`
	TotalPages  int32            `json:"totalPages"`
	TotalItems  int32            `json:"totalItems"`
	CurrentPage int32            `json:"currentPage"`
	PageSize    int32            `json:"pageSize"`
	HasNextPage bool             `json:"hasNextPage"`
}

type createCourseRequest struct {
	Code        string  `json:"code" validate:"required,min=1,max=100"`
	Title       string  `json:"title" validate:"required,min=1,max=200"`
	Description *string `json:"description,omitempty" validate:"omitempty,max=4000"`
}

type updateCourseRequest struct {
	Code        string  `json:"code" validate:"required,min=1,max=100"`
	Title       string  `json:"title" validate:"required,min=1,max=200"`
	Description *string `json:"description,omitempty" validate:"omitempty,max=4000"`
}

type updateCourseStatusRequest struct {
	Status CourseStatus `json:"status" validate:"required,oneof=DRAFT PUBLISHED ARCHIVED"`
}

func NewHandler(service *Service, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Handler{
		service: service,
		logger:  logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			var syntaxErr *json.SyntaxError
			var typeErr *json.UnmarshalTypeError
			switch {
			case errors.Is(err, errInvalidCoursePayload), errors.Is(err, io.EOF), errors.As(err, &syntaxErr), errors.As(err, &typeErr):
				return problemutil.NewValidateProblem(err.Error())
			case errors.Is(err, databaseutil.ErrUniqueViolation):
				return problemutil.Problem{
					Title:  "Conflict",
					Status: http.StatusConflict,
					Type:   "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/409",
					Detail: err.Error(),
				}
			}
			return problemutil.Problem{}
		}),
		validator: validator.New(),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set, authorizer *auth.Authorizer) {
	managementOnly := middlewares.Append(authorizer.RequireAnyRole(auth.EXPERIMENTER, auth.ADMIN))
	handle := func(pattern string, set *middlewareutil.Set, fn http.HandlerFunc) {
		if set != nil {
			fn = set.HandlerFunc(fn)
		}
		mux.HandleFunc(pattern, fn)
	}

	handle("GET /api/courses", managementOnly, h.List)
	handle("POST /api/courses", managementOnly, h.Create)
	handle("GET /api/courses/{id}", middlewares, h.Get)
	handle("PUT /api/courses/{id}", managementOnly, h.Update)
	handle("PUT /api/courses/{id}/status", managementOnly, h.UpdateStatus)
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

	items := make([]courseResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, buildCourseResponse(item))
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, paginatedCoursesResponse{
		Items:       items,
		TotalPages:  page.TotalPages,
		TotalItems:  page.TotalItems,
		CurrentPage: page.CurrentPage,
		PageSize:    page.PageSize,
		HasNextPage: page.HasNextPage,
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	var request createCourseRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &request); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	record, err := h.service.Create(ctx, CreateParams(request))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusCreated, buildCourseResponse(record))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	actorID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	record, err := h.service.ByIDForActor(ctx, actorID, id)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, buildCourseResponse(record))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	var request updateCourseRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &request); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	record, err := h.service.Update(ctx, UpdateParams{
		ID: id, Code: request.Code, Title: request.Title, Description: request.Description,
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, buildCourseResponse(record))
}

func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	var request updateCourseStatusRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &request); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	record, err := h.service.UpdateStatus(ctx, id, request.Status)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, buildCourseResponse(record))
}

func parseListInput(r *http.Request) (ListInput, error) {
	query := r.URL.Query()
	page, err := parseIntQuery(query.Get("page"), defaultPage)
	if err != nil || page < 1 {
		return ListInput{}, fmt.Errorf("%w: page must be a positive integer", errInvalidCoursePayload)
	}
	pageSize, err := parseIntQuery(query.Get("pageSize"), defaultPageSize)
	if err != nil || pageSize < 1 || pageSize > maxPageSize {
		return ListInput{}, fmt.Errorf("%w: pageSize must be between 1 and %d", errInvalidCoursePayload, maxPageSize)
	}

	var status *CourseStatus
	if raw := query.Get("status"); raw != "" {
		value := CourseStatus(raw)
		if !validCourseStatus(value) {
			return ListInput{}, fmt.Errorf("%w: unknown course status", errInvalidCoursePayload)
		}
		status = &value
	}

	var search *string
	if raw := strings.TrimSpace(query.Get("search")); raw != "" {
		if len([]rune(raw)) > maxCourseSearchLength {
			return ListInput{}, fmt.Errorf("%w: search must be at most %d characters", errInvalidCoursePayload, maxCourseSearchLength)
		}
		search = &raw
	}

	return ListInput{Page: page, PageSize: pageSize, Status: status, Search: search}, nil
}

func parseIntQuery(raw string, fallback int32) (int32, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(value), nil
}

func buildCourseResponse(record Record) courseResponse {
	return courseResponse(record)
}
