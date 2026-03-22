package question

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	middlewareutil "github.com/NYCU-SDC/summer/pkg/middleware"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var errInvalidQuestionPayload = errors.New("invalid question payload")

type Handler struct {
	questionService *QuestionService
	optionService   *OptionService
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

func NewHandler(questionService *QuestionService, optionService *OptionService, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handler{
		questionService: questionService,
		optionService:   optionService,
		logger:          logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			if errors.Is(err, errInvalidQuestionPayload) {
				return problemutil.NewValidateProblem(err.Error())
			}
			return problemutil.Problem{}
		}),
		validator: validator.New(),
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set) {
	handle := func(pattern string, fn http.HandlerFunc) {
		if middlewares != nil {
			fn = middlewares.HandlerFunc(fn)
		}
		mux.HandleFunc(pattern, fn)
	}

	handle("GET /api/questions", h.List)
	handle("POST /api/questions", h.Create)
	handle("GET /api/questions/{id}", h.Get)
	handle("PUT /api/questions/{id}", h.Update)
	handle("DELETE /api/questions/{id}", h.Delete)
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

	question, err := h.questionService.Create(ctx, QuestionRequest{
		Type:    req.Type,
		Content: req.Content,
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if err := h.syncQuestionOptions(ctx, question.ID, req.Type, req.Options, false); err != nil {
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

	if _, err := h.questionService.Get(ctx, id); err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	question, err := h.questionService.Update(ctx, id, QuestionRequest{
		Type:    req.Type,
		Content: req.Content,
	})
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	if err := h.syncQuestionOptions(ctx, id, req.Type, req.Options, true); err != nil {
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

func (h *Handler) buildQuestionResponse(ctx context.Context, q Question) (questionResponse, error) {
	resp := questionResponse{
		ID:      q.ID,
		Type:    q.Type,
		Content: q.Content,
	}

	if q.Type != "CHOICE" {
		return resp, nil
	}

	opts, err := h.optionService.ListByQuestion(ctx, q.ID)
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

func (h *Handler) syncQuestionOptions(ctx context.Context, questionID uuid.UUID, questionType string, options []createUpdateOptionRequest, replace bool) error {
	if replace || questionType == "TEXT" {
		existing, err := h.optionService.ListByQuestion(ctx, questionID)
		if err != nil {
			return err
		}
		for _, opt := range existing {
			if err := h.optionService.Delete(ctx, opt.ID); err != nil {
				return err
			}
		}
	}

	if questionType == "TEXT" {
		return nil
	}

	if len(options) == 0 {
		return fmt.Errorf("%w: options are required for CHOICE question", errInvalidQuestionPayload)
	}

	for _, opt := range options {
		if _, err := h.optionService.Create(ctx, OptionRequest{
			QuestionID: questionID,
			Label:      opt.Label,
			Content:    opt.Content,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (h *Handler) parseID(raw string) (uuid.UUID, error) {
	return handlerutil.ParseUUID(raw)
}
