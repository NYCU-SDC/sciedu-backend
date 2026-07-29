package question

import (
	"context"
	"errors"
	"net/http"
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
	questionService *QuestionService
	answerService   *AnswerService
	logger          *zap.Logger
	problemWriter   *problemutil.HttpWriter
	validator       *validator.Validate
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

type answerResponse struct {
	ID               uuid.UUID  `json:"id"`
	QuestionID       uuid.UUID  `json:"questionId"`
	UserID           uuid.UUID  `json:"userId"`
	SelectedOptionID *uuid.UUID `json:"selectedOptionId,omitempty"`
	TextAnswer       *string    `json:"textAnswer,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
}

func NewHandler(questionService *QuestionService, answerService *AnswerService, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handler{
		questionService: questionService,
		answerService:   answerService,
		logger:          logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			if errors.Is(err, errInvalidQuestionPayload) || errors.Is(err, errInvalidAnswerPayload) {
				return problemutil.NewValidateProblem(err.Error())
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
	if middlewares != nil && authorizer != nil {
		answerReadAccess = middlewares.Append(authorizer.RequireAnyRole(auth.EXPERIMENTER, auth.ADMIN))
	}

	handle("GET /api/questions", middlewares, h.List)
	handle("POST /api/questions", middlewares, h.Create)
	handle("GET /api/questions/{id}", middlewares, h.Get)
	handle("PUT /api/questions/{id}", middlewares, h.Update)
	handle("DELETE /api/questions/{id}", middlewares, h.Delete)
	handle("POST /api/questions/{id}/answers", middlewares, h.SubmitAnswer)
	handle("GET /api/questions/{id}/answers", answerReadAccess, h.ListAnswers)
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

	answer, err := h.answerService.Create(ctx, AnswerRequest{
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

	answers, err := h.answerService.ListByQuestion(ctx, questionID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	resp := make([]answerResponse, 0, len(answers))
	for _, answer := range answers {
		resp = append(resp, buildAnswerResponse(answer))
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, resp)
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
		ID:         answer.ID,
		QuestionID: answer.QuestionID,
		UserID:     answer.UserID,
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

func (r createUpdateQuestionRequest) toQuestionOptionRequests() []QuestionOptionRequest {
	options := make([]QuestionOptionRequest, 0, len(r.Options))
	for _, opt := range r.Options {
		options = append(options, QuestionOptionRequest(opt))
	}
	return options
}
