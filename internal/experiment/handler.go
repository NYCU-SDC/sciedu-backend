package experiment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"sciedu-backend/internal/auth"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultPage     int32 = 1
	defaultPageSize int32 = 20
	maxPageSize     int32 = 100
	maxSearchLength       = 200
)

type HandlerService interface {
	List(ctx context.Context, input ListInput) (ExperimentPage, error)
	Create(ctx context.Context, createdBy uuid.UUID, params EditableParams) (Record, error)
	FindByID(ctx context.Context, id uuid.UUID) (Record, error)
	Update(ctx context.Context, id uuid.UUID, params EditableParams) (Record, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (Record, error)
	ListParticipants(ctx context.Context, experimentID uuid.UUID, input ParticipantListInput) (ParticipantPage, error)
	AddParticipants(ctx context.Context, experimentID uuid.UUID, userIDs []uuid.UUID) ([]ParticipantAssignment, error)
	RemoveParticipant(ctx context.Context, experimentID, userID uuid.UUID) error
}

type Handler struct {
	service       HandlerService
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
	validator     *validator.Validate
}

type configurationRequest struct {
	MaxAttempts              *int32                    `json:"maxAttempts" validate:"required,gte=1"`
	AllowRetry               *bool                     `json:"allowRetry" validate:"required"`
	ShowScore                *bool                     `json:"showScore" validate:"required"`
	ShowExplanations         *bool                     `json:"showExplanations" validate:"required"`
	GradingMode              *GradingMode              `json:"gradingMode" validate:"required,oneof=AUTOMATIC MANUAL"`
	CorrectAnswerReleaseMode *CorrectAnswerReleaseMode `json:"correctAnswerReleaseMode" validate:"required,oneof=AFTER_PAGE_SUBMISSION AFTER_COURSE_COMPLETION NEVER"`
}

type editableExperimentRequest struct {
	Name             string               `json:"name" validate:"required,min=1,max=200"`
	Description      *string              `json:"description,omitempty" validate:"omitempty,max=4000"`
	ScheduledStartAt time.Time            `json:"scheduledStartAt" validate:"required"`
	ScheduledEndAt   time.Time            `json:"scheduledEndAt" validate:"required"`
	Configuration    configurationRequest `json:"configuration" validate:"required"`
}

type updateExperimentStatusRequest struct {
	Status *Status `json:"status" validate:"required,oneof=DRAFT SCHEDULED ACTIVE COMPLETED ARCHIVED"`
}

type addExperimentParticipantsRequest struct {
	UserIDs []string `json:"userIds" validate:"required,min=1,max=100,dive,required"`
}

type experimentResponse struct {
	ID               uuid.UUID     `json:"id"`
	Name             string        `json:"name"`
	Description      *string       `json:"description,omitempty"`
	ScheduledStartAt time.Time     `json:"scheduledStartAt"`
	ScheduledEndAt   time.Time     `json:"scheduledEndAt"`
	Status           Status        `json:"status"`
	Configuration    Configuration `json:"configuration"`
	CreatedBy        uuid.UUID     `json:"createdBy"`
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
}

type experimentDetailResponse struct {
	experimentResponse
	ParticipantCount int32 `json:"participantCount"`
	CourseCount      int32 `json:"courseCount"`
}

type paginatedExperimentsResponse struct {
	Items       []experimentResponse `json:"items"`
	TotalPages  int32                `json:"totalPages"`
	TotalItems  int32                `json:"totalItems"`
	CurrentPage int32                `json:"currentPage"`
	PageSize    int32                `json:"pageSize"`
	HasNextPage bool                 `json:"hasNextPage"`
}

type participantResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL *string   `json:"avatarUrl,omitempty"`
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type participantAssignmentResponse struct {
	Participant participantResponse `json:"participant"`
	AssignedAt  time.Time           `json:"assignedAt"`
}

type paginatedExperimentParticipantsResponse struct {
	Items       []participantAssignmentResponse `json:"items"`
	TotalPages  int32                           `json:"totalPages"`
	TotalItems  int32                           `json:"totalItems"`
	CurrentPage int32                           `json:"currentPage"`
	PageSize    int32                           `json:"pageSize"`
	HasNextPage bool                            `json:"hasNextPage"`
}

func NewHandler(service HandlerService, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Handler{
		service: service,
		logger:  logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			var syntaxError *json.SyntaxError
			var typeError *json.UnmarshalTypeError
			switch {
			case errors.Is(err, errInvalidExperimentPayload):
				return problemutil.NewValidateProblem(err.Error())
			case errors.Is(err, errExperimentParticipantConflict):
				return problemutil.Problem{
					Title:  "Conflict",
					Status: http.StatusConflict,
					Type:   "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/409",
					Detail: errExperimentParticipantConflict.Error(),
				}
			case errors.As(err, &syntaxError), errors.As(err, &typeError), errors.Is(err, io.EOF):
				return problemutil.NewValidateProblem("invalid JSON request body")
			default:
				return problemutil.Problem{}
			}
		}),
		validator: validator.New(),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set, authorizer *auth.Authorizer) {
	managementAccess := middlewares.Append(authorizer.RequireAnyRole(auth.EXPERIMENTER, auth.ADMIN))
	handle := func(pattern string, fn http.HandlerFunc) {
		mux.HandleFunc(pattern, managementAccess.HandlerFunc(fn))
	}

	handle("GET /api/experiments", h.List)
	handle("POST /api/experiments", h.Create)
	handle("GET /api/experiments/{id}", h.Get)
	handle("PUT /api/experiments/{id}", h.Update)
	handle("PUT /api/experiments/{id}/status", h.UpdateStatus)
	handle("GET /api/experiments/{id}/participants", h.ListParticipants)
	handle("POST /api/experiments/{id}/participants", h.AddParticipants)
	handle("DELETE /api/experiments/{id}/participants/{userId}", h.RemoveParticipant)
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
	items := make([]experimentResponse, 0, len(page.Items))
	for _, record := range page.Items {
		items = append(items, responseFromRecord(record))
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, paginatedExperimentsResponse{
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
	createdBy, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}
	params, err := h.parseEditableRequest(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	record, err := h.service.Create(ctx, createdBy, params)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusCreated, detailResponseFromRecord(record))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	record, err := h.service.FindByID(ctx, id)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, detailResponseFromRecord(record))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	params, err := h.parseEditableRequest(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	record, err := h.service.Update(ctx, id, params)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, detailResponseFromRecord(record))
}

func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	id, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	var request updateExperimentStatusRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &request); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	record, err := h.service.UpdateStatus(ctx, id, *request.Status)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, detailResponseFromRecord(record))
}

func (h *Handler) ListParticipants(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	experimentID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	input, err := parseParticipantListInput(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	page, err := h.service.ListParticipants(ctx, experimentID, input)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	items := make([]participantAssignmentResponse, 0, len(page.Items))
	for _, assignment := range page.Items {
		items = append(items, participantAssignmentResponseFromAssignment(assignment))
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, paginatedExperimentParticipantsResponse{
		Items:       items,
		TotalPages:  page.TotalPages,
		TotalItems:  page.TotalItems,
		CurrentPage: page.CurrentPage,
		PageSize:    page.PageSize,
		HasNextPage: page.HasNextPage,
	})
}

func (h *Handler) AddParticipants(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	experimentID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	var request addExperimentParticipantsRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &request); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	userIDs := make([]uuid.UUID, 0, len(request.UserIDs))
	for _, rawID := range request.UserIDs {
		userID, err := uuid.Parse(rawID)
		if err != nil {
			h.problemWriter.WriteError(
				ctx,
				w,
				fmt.Errorf("%w: userIds must contain valid UUIDs", errInvalidExperimentPayload),
				logger,
			)
			return
		}
		userIDs = append(userIDs, userID)
	}

	assignments, err := h.service.AddParticipants(ctx, experimentID, userIDs)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	response := make([]participantAssignmentResponse, 0, len(assignments))
	for _, assignment := range assignments {
		response = append(response, participantAssignmentResponseFromAssignment(assignment))
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, response)
}

func (h *Handler) RemoveParticipant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)
	experimentID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	userID, err := handlerutil.ParseUUID(r.PathValue("userId"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	if err := h.service.RemoveParticipant(ctx, experimentID, userID); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) parseEditableRequest(r *http.Request) (EditableParams, error) {
	var request editableExperimentRequest
	if err := handlerutil.ParseAndValidateRequestBody(r.Context(), h.validator, r, &request); err != nil {
		return EditableParams{}, err
	}
	return EditableParams{
		Name:             request.Name,
		Description:      request.Description,
		ScheduledStartAt: request.ScheduledStartAt,
		ScheduledEndAt:   request.ScheduledEndAt,
		Configuration: Configuration{
			MaxAttempts:              *request.Configuration.MaxAttempts,
			AllowRetry:               *request.Configuration.AllowRetry,
			ShowScore:                *request.Configuration.ShowScore,
			ShowExplanations:         *request.Configuration.ShowExplanations,
			GradingMode:              *request.Configuration.GradingMode,
			CorrectAnswerReleaseMode: *request.Configuration.CorrectAnswerReleaseMode,
		},
	}, nil
}

func parseListInput(r *http.Request) (ListInput, error) {
	query := r.URL.Query()
	page, err := parseInt32Query(query.Get("page"), query.Has("page"), defaultPage)
	if err != nil || page < 1 {
		return ListInput{}, fmt.Errorf("%w: page must be a positive integer", errInvalidExperimentPayload)
	}
	pageSize, err := parseInt32Query(query.Get("pageSize"), query.Has("pageSize"), defaultPageSize)
	if err != nil || pageSize < 1 || pageSize > maxPageSize {
		return ListInput{}, fmt.Errorf("%w: pageSize must be between 1 and %d", errInvalidExperimentPayload, maxPageSize)
	}

	var status *Status
	if query.Has("status") {
		value := Status(query.Get("status"))
		if !value.Valid() {
			return ListInput{}, fmt.Errorf("%w: unknown experiment status", errInvalidExperimentPayload)
		}
		status = &value
	}
	scheduledFrom, err := parseTimeQuery(query.Get("scheduledFrom"), query.Has("scheduledFrom"), "scheduledFrom")
	if err != nil {
		return ListInput{}, err
	}
	scheduledTo, err := parseTimeQuery(query.Get("scheduledTo"), query.Has("scheduledTo"), "scheduledTo")
	if err != nil {
		return ListInput{}, err
	}
	var search *string
	if query.Has("search") {
		value := query.Get("search")
		if value == "" || utf8.RuneCountInString(value) > maxSearchLength {
			return ListInput{}, fmt.Errorf("%w: search must contain between 1 and %d characters", errInvalidExperimentPayload, maxSearchLength)
		}
		search = &value
	}
	return ListInput{
		Page:          page,
		PageSize:      pageSize,
		Status:        status,
		ScheduledFrom: scheduledFrom,
		ScheduledTo:   scheduledTo,
		Search:        search,
	}, nil
}

func parseInt32Query(raw string, present bool, fallback int32) (int32, error) {
	if !present {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	return int32(value), err
}

func parseTimeQuery(raw string, present bool, name string) (*time.Time, error) {
	if !present {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be an RFC3339 timestamp", errInvalidExperimentPayload, name)
	}
	return &value, nil
}

func parseParticipantListInput(r *http.Request) (ParticipantListInput, error) {
	query := r.URL.Query()
	page, err := parseInt32Query(query.Get("page"), query.Has("page"), defaultPage)
	if err != nil || page < 1 {
		return ParticipantListInput{}, fmt.Errorf("%w: page must be a positive integer", errInvalidExperimentPayload)
	}
	pageSize, err := parseInt32Query(query.Get("pageSize"), query.Has("pageSize"), defaultPageSize)
	if err != nil || pageSize < 1 || pageSize > maxPageSize {
		return ParticipantListInput{}, fmt.Errorf(
			"%w: pageSize must be between 1 and %d",
			errInvalidExperimentPayload,
			maxPageSize,
		)
	}
	return ParticipantListInput{Page: page, PageSize: pageSize}, nil
}

func responseFromRecord(record Record) experimentResponse {
	return experimentResponse{
		ID:               record.ID,
		Name:             record.Name,
		Description:      record.Description,
		ScheduledStartAt: record.ScheduledStartAt,
		ScheduledEndAt:   record.ScheduledEndAt,
		Status:           record.Status,
		Configuration:    record.Configuration,
		CreatedBy:        record.CreatedBy,
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}

func detailResponseFromRecord(record Record) experimentDetailResponse {
	return experimentDetailResponse{
		experimentResponse: responseFromRecord(record),
		ParticipantCount:   record.ParticipantCount,
		CourseCount:        record.CourseCount,
	}
}

func participantAssignmentResponseFromAssignment(assignment ParticipantAssignment) participantAssignmentResponse {
	participant := assignment.Participant
	return participantAssignmentResponse{
		Participant: participantResponse(participant),
		AssignedAt:  assignment.AssignedAt,
	}
}
