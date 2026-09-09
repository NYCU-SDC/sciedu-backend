package question

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sciedu-backend/internal/auth"
)

type Handler struct {
	resultService        *AnswerResultService
	resultRoles          auth.RoleQuerier
	submission           AnswerSubmitter
	questionService      *QuestionService
	answerService        *AnswerService
	correctAnswerService *CorrectAnswerService
	logger               *zap.Logger
	problemWriter        *problemutil.HttpWriter
	validator            *validator.Validate
}

func (h *Handler) WithResults(service *AnswerResultService, roles auth.RoleQuerier) *Handler {
	h.resultService, h.resultRoles = service, roles
	return h
}

type AnswerSubmitter interface {
	Submit(context.Context, AnswerRequest) (Answer, error)
}

// WithSubmission installs the scoped submission boundary; production must supply it.
func (h *Handler) WithSubmission(submission AnswerSubmitter) *Handler {
	h.submission = submission
	return h
}

type createUpdateOptionRequest struct {
	Label   string `json:"label" validate:"required,min=1,max=5"`
	Content string `json:"content" validate:"required,min=1,max=1024"`
}

type createUpdateQuestionRequest struct {
	Type    string                      `json:"type" validate:"required,oneof=CHOICE TEXT"`
	Content string                      `json:"content" validate:"required,min=1,max=2000"`
	Options []createUpdateOptionRequest `json:"options" validate:"required_if=Type CHOICE,dive"`
}

type optionResponse struct {
	ID      uuid.UUID `json:"id"`
	Label   string    `json:"label"`
	Content string    `json:"content"`
}

type questionResponse struct {
	ID      uuid.UUID        `json:"id"`
	Type    string           `json:"type"`
	Content string           `json:"content"`
	Options []optionResponse `json:"options,omitempty"`
}

type submitAnswerRequest struct {
	SelectedOptionID *uuid.UUID `json:"selectedOptionId"`
	TextAnswer       *string    `json:"textAnswer" validate:"omitempty,min=1,max=2000"`
}

func (r *submitAnswerRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for field := range fields {
		if strings.EqualFold(field, "experimentId") {
			return fmt.Errorf("%w: experimentId is resolved by the server", errInvalidAnswerPayload)
		}
	}
	type payload submitAnswerRequest
	return json.Unmarshal(data, (*payload)(r))
}

type answerResponse struct {
	ID               uuid.UUID  `json:"id"`
	QuestionID       uuid.UUID  `json:"questionId"`
	UserID           uuid.UUID  `json:"userId"`
	ExperimentID     uuid.UUID  `json:"experimentId"`
	SelectedOptionID *uuid.UUID `json:"selectedOptionId,omitempty"`
	TextAnswer       *string    `json:"textAnswer,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
}

type answerWithResultResponse struct {
	answerResponse
	GradingStatus string     `json:"gradingStatus"`
	GradingMethod *string    `json:"gradingMethod,omitempty"`
	IsCorrect     *bool      `json:"isCorrect,omitempty"`
	GradedAt      *time.Time `json:"gradedAt,omitempty"`
}

type answerResultResponse struct {
	AnswerID      uuid.UUID  `json:"answerId"`
	QuestionID    uuid.UUID  `json:"questionId"`
	Status        string     `json:"status"`
	Method        *string    `json:"method,omitempty"`
	ResultVisible bool       `json:"resultVisible"`
	IsCorrect     *bool      `json:"isCorrect,omitempty"`
	GradedAt      *time.Time `json:"gradedAt,omitempty"`
}

type paginatedAnswersResponse struct {
	Items       []answerWithResultResponse `json:"items"`
	TotalPages  int32                      `json:"totalPages"`
	TotalItems  int32                      `json:"totalItems"`
	CurrentPage int32                      `json:"currentPage"`
	PageSize    int32                      `json:"pageSize"`
	HasNextPage bool                       `json:"hasNextPage"`
}

type upsertCorrectAnswerRequest struct {
	Type             string     `json:"type" validate:"required,oneof=CHOICE TEXT"`
	SelectedOptionID *uuid.UUID `json:"selectedOptionId"`
	ReferenceAnswer  *string    `json:"referenceAnswer"`
}

type correctAnswerResponse struct {
	QuestionID             uuid.UUID  `json:"questionId"`
	Type                   string     `json:"type"`
	SelectedOptionID       *uuid.UUID `json:"selectedOptionId,omitempty"`
	ReferenceAnswer        *string    `json:"referenceAnswer,omitempty"`
	Version                int64      `json:"version"`
	AnswerResultSyncStatus string     `json:"answerResultSyncStatus"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

func NewHandler(
	questionService *QuestionService,
	answerService *AnswerService,
	correctAnswerService *CorrectAnswerService,
	logger *zap.Logger,
) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handler{
		questionService:      questionService,
		answerService:        answerService,
		correctAnswerService: correctAnswerService,
		logger:               logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			if errors.Is(err, errInvalidQuestionPayload) ||
				errors.Is(err, errInvalidAnswerPayload) ||
				errors.Is(err, errInvalidCorrectAnswerPayload) {
				return problemutil.NewValidateProblem(err.Error())
			}
			// A question referenced by a page block is protected by ON DELETE
			// RESTRICT, which summer leaves unmapped.
			if errors.Is(err, errQuestionReferenced) || errors.Is(err, errDuplicateAnswer) {
				detail := errQuestionReferenced.Error()
				if errors.Is(err, errDuplicateAnswer) {
					detail = errDuplicateAnswer.Error()
				}
				return problemutil.Problem{
					Title:  "Conflict",
					Status: http.StatusConflict,
					Type:   "https://developer.mozilla.org/en-US/docs/Web/HTTP/Status/409",
					Detail: detail,
				}
			}
			return problemutil.Problem{}
		}),
		validator: validator.New(),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set, authorizer *auth.Authorizer) {
	handle := func(pattern string, set *middlewareutil.Set, fn http.HandlerFunc) {
		if set != nil {
			fn = set.HandlerFunc(fn)
		}
		mux.HandleFunc(pattern, fn)
	}

	answerReadAccess := middlewares
	correctAnswerAccess := middlewares
	if middlewares != nil && authorizer != nil {
		answerReadAccess = middlewares.Append(authorizer.RequireAnyRole(auth.EXPERIMENTER, auth.ADMIN))
		correctAnswerAccess = middlewares.Append(authorizer.RequireAnyRole(auth.EXPERIMENTER, auth.ADMIN))
	}

	handle("GET /api/questions", middlewares, h.List)
	handle("POST /api/questions", middlewares, h.Create)
	handle("GET /api/questions/{id}", middlewares, h.Get)
	handle("PUT /api/questions/{id}", middlewares, h.Update)
	handle("DELETE /api/questions/{id}", middlewares, h.Delete)
	handle("GET /api/questions/{id}/correct-answer", correctAnswerAccess, h.GetCorrectAnswer)
	handle("PUT /api/questions/{id}/correct-answer", correctAnswerAccess, h.UpsertCorrectAnswer)
	handle("POST /api/questions/{id}/answers", middlewares, h.SubmitAnswer)
	handle("GET /api/questions/{id}/answers", answerReadAccess, h.ListAnswers)
	handle("GET /api/questions/{questionId}/answers/{answerId}/result", middlewares, h.GetAnswerResult)
}

func (h *Handler) GetCorrectAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	questionID, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	answer, err := h.correctAnswerService.Get(ctx, questionID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildCorrectAnswerResponse(answer))
}

func (h *Handler) UpsertCorrectAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	questionID, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	var request upsertCorrectAnswerRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &request); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	answer, err := h.correctAnswerService.Upsert(ctx, questionID, CorrectAnswerRequest(request))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, buildCorrectAnswerResponse(answer))
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	questions, err := h.questionService.List(ctx)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	resp := make([]questionResponse, 0, len(questions))
	for _, q := range questions {
		item, err := h.buildQuestionResponse(ctx, q)
		if err != nil {
			h.problemWriter.WriteError(ctx, w, err, logger)
			return
		}
		resp = append(resp, item)
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, resp)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	question, err := h.questionService.Get(ctx, id)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	resp, err := h.buildQuestionResponse(ctx, question)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, resp)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	var req createUpdateQuestionRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	question, err := h.questionService.CreateWithOptions(ctx, QuestionRequest{
		Type:    req.Type,
		Content: req.Content,
	}, req.toQuestionOptionRequests())
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	resp, err := h.buildQuestionResponse(ctx, question)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusCreated, resp)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	var req createUpdateQuestionRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	question, err := h.questionService.UpdateWithOptions(ctx, id, QuestionRequest{
		Type:    req.Type,
		Content: req.Content,
	}, req.toQuestionOptionRequests())
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	resp, err := h.buildQuestionResponse(ctx, question)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, resp)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	id, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if _, err := h.questionService.Get(ctx, id); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if err := h.questionService.Delete(ctx, id); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	questionID, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	var req submitAnswerRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if h.submission == nil {
		h.problemWriter.WriteError(ctx, w, errors.New("answer submission is not configured"), logger)
		return
	}
	answer, err := h.submission.Submit(ctx, AnswerRequest{
		QuestionID:       questionID,
		UserID:           userID,
		SelectedOptionID: req.SelectedOptionID,
		TextAnswer:       req.TextAnswer,
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusCreated, buildAnswerResponse(answer))
}

func (h *Handler) ListAnswers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	questionID, err := h.parseID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	input, err := parseAnswerListInput(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	page, err := h.answerService.ListByQuestion(ctx, questionID, input)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	items := make([]answerWithResultResponse, 0, len(page.Items))
	for _, answer := range page.Items {
		items = append(items, buildAnswerWithResultResponse(answer))
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, paginatedAnswersResponse{
		Items:       items,
		TotalPages:  page.TotalPages,
		TotalItems:  page.TotalItems,
		CurrentPage: page.CurrentPage,
		PageSize:    page.PageSize,
		HasNextPage: page.HasNextPage,
	})
}

func parseAnswerListInput(r *http.Request) (AnswerListInput, error) {
	query := r.URL.Query()
	experimentID, err := handlerutil.ParseUUID(query.Get("experimentId"))
	if err != nil {
		return AnswerListInput{}, fmt.Errorf("%w: experimentId is required and must be a UUID", errInvalidAnswerPayload)
	}

	parse := func(name string, defaultValue int32) (int32, error) {
		if !query.Has(name) {
			return defaultValue, nil
		}
		raw := query.Get(name)
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("%w: %s must be an integer", errInvalidAnswerPayload, name)
		}
		return int32(value), nil
	}

	page, err := parse("page", 1)
	if err != nil {
		return AnswerListInput{}, err
	}
	pageSize, err := parse("pageSize", 20)
	if err != nil {
		return AnswerListInput{}, err
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return AnswerListInput{}, fmt.Errorf("%w: page must be positive and pageSize must be between 1 and 100", errInvalidAnswerPayload)
	}
	return AnswerListInput{ExperimentID: experimentID, Page: page, PageSize: pageSize}, nil
}

func (h *Handler) parseID(raw string) (uuid.UUID, error) {
	return handlerutil.ParseUUID(raw)
}

func (h *Handler) buildQuestionResponse(ctx context.Context, q Question) (questionResponse, error) {
	resp := questionResponse{
		ID:      q.ID,
		Type:    q.Type,
		Content: q.Content,
	}

	if q.Type != "CHOICE" {
		return resp, nil
	}

	opts, err := h.questionService.ListOptionsByQuestion(ctx, q.ID)
	if err != nil {
		return questionResponse{}, err
	}

	resp.Options = make([]optionResponse, 0, len(opts))
	for _, opt := range opts {
		resp.Options = append(resp.Options, optionResponse{
			ID:      opt.ID,
			Label:   opt.Label,
			Content: opt.Content,
		})
	}

	return resp, nil
}

func buildAnswerResponse(answer Answer) answerResponse {
	resp := answerResponse{
		ID:           answer.ID,
		QuestionID:   answer.QuestionID,
		UserID:       answer.UserID,
		ExperimentID: answer.ExperimentID,
	}

	if answer.SelectedOptionID.Valid {
		selectedOptionID := uuid.UUID(answer.SelectedOptionID.Bytes)
		resp.SelectedOptionID = &selectedOptionID
	}

	if answer.TextAnswer.Valid {
		textAnswer := answer.TextAnswer.String
		resp.TextAnswer = &textAnswer
	}

	if answer.CreatedAt.Valid {
		resp.CreatedAt = answer.CreatedAt.Time
	}

	return resp
}

func buildCorrectAnswerResponse(answer CorrectAnswer) correctAnswerResponse {
	response := correctAnswerResponse{
		QuestionID:             answer.QuestionID,
		Type:                   answer.Type,
		Version:                answer.Version,
		AnswerResultSyncStatus: answer.AnswerResultSyncStatus,
	}
	if answer.SelectedOptionID.Valid {
		selectedOptionID := uuid.UUID(answer.SelectedOptionID.Bytes)
		response.SelectedOptionID = &selectedOptionID
	}
	if answer.ReferenceAnswer.Valid {
		referenceAnswer := answer.ReferenceAnswer.String
		response.ReferenceAnswer = &referenceAnswer
	}
	if answer.UpdatedAt.Valid {
		response.UpdatedAt = answer.UpdatedAt.Time
	}
	return response
}

func buildAnswerWithResultResponse(answer AnswerWithResult) answerWithResultResponse {
	response := answerWithResultResponse{
		answerResponse: answerResponse{
			ID:               answer.ID,
			QuestionID:       answer.QuestionID,
			UserID:           answer.UserID,
			ExperimentID:     answer.ExperimentID,
			SelectedOptionID: answer.SelectedOptionID,
			TextAnswer:       answer.TextAnswer,
			CreatedAt:        answer.CreatedAt,
		},
		GradingStatus: string(answer.GradingStatus),
		IsCorrect:     answer.IsCorrect,
		GradedAt:      answer.GradedAt,
	}
	if answer.GradingMethod != nil {
		method := string(*answer.GradingMethod)
		response.GradingMethod = &method
	}
	return response
}

func buildAnswerResultResponse(result AnswerResultView) answerResultResponse {
	response := answerResultResponse{
		AnswerID:      result.AnswerID,
		QuestionID:    result.QuestionID,
		Status:        string(result.Status),
		ResultVisible: result.ResultVisible,
		IsCorrect:     result.IsCorrect,
		GradedAt:      result.GradedAt,
	}
	if result.Method != nil {
		method := string(*result.Method)
		response.Method = &method
	}
	return response
}

func (r createUpdateQuestionRequest) toQuestionOptionRequests() []QuestionOptionRequest {
	options := make([]QuestionOptionRequest, 0, len(r.Options))
	for _, opt := range r.Options {
		options = append(options, QuestionOptionRequest(opt))
	}
	return options
}
