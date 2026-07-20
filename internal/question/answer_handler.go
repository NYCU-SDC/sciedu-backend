package question

import (
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

type AnswerHandler struct {
	answerService *AnswerService
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
	validator     *validator.Validate
}

type submitAnswerRequest struct {
	SelectedOptionID *uuid.UUID `json:"selectedOptionId"`
	TextAnswer       *string    `json:"textAnswer"`
}

type answerResponse struct {
	ID               uuid.UUID  `json:"id"`
	QuestionID       uuid.UUID  `json:"questionId"`
	SelectedOptionID *uuid.UUID `json:"selectedOptionId"`
	TextAnswer       *string    `json:"textAnswer"`
	CreatedAt        time.Time  `json:"createdAt"`
}

func NewAnswerHandler(answerService *AnswerService, logger *zap.Logger) *AnswerHandler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &AnswerHandler{
		answerService: answerService,
		logger:        logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			if errors.Is(err, errInvalidAnswerPayload) {
				return problemutil.NewValidateProblem(err.Error())
			}
			return problemutil.Problem{}
		}),
		validator: validator.New(),
	}
}

func (h *AnswerHandler) RegisterRoutes(mux *http.ServeMux, middlewares *middlewareutil.Set) {
	handle := func(pattern string, fn http.HandlerFunc) {
		if middlewares != nil {
			fn = middlewares.HandlerFunc(fn)
		}
		mux.HandleFunc(pattern, fn)
	}

	handle("POST /api/questions/{id}/answers", h.Submit)
	handle("GET /api/questions/{id}/answers", h.List)
}

func (h *AnswerHandler) Submit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	questionID, err := handlerutil.ParseUUID(r.PathValue("id"))
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

func (h *AnswerHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	questionID, err := handlerutil.ParseUUID(r.PathValue("id"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	answers, err := h.answerService.ListByQuestionForUser(ctx, questionID, userID)
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

func buildAnswerResponse(answer Answer) answerResponse {
	resp := answerResponse{
		ID:         answer.ID,
		QuestionID: answer.QuestionID,
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
