package question

import (
	"errors"
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

func NewHandler(questionService *QuestionService, logger *zap.Logger) *Handler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Handler{
		questionService: questionService,
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
		item, err := h.questionService.buildQuestionResponse(ctx, q)
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

	resp, err := h.questionService.buildQuestionResponse(ctx, question)
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

	resp, err := h.questionService.buildQuestionResponse(ctx, question)
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

	resp, err := h.questionService.buildQuestionResponse(ctx, question)
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

func (h *Handler) parseID(raw string) (uuid.UUID, error) {
	return handlerutil.ParseUUID(raw)
}

func (r createUpdateQuestionRequest) toQuestionOptionRequests() []QuestionOptionRequest {
	options := make([]QuestionOptionRequest, 0, len(r.Options))
	for _, opt := range r.Options {
		options = append(options, QuestionOptionRequest(opt))
	}
	return options
}
