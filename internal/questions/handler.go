package questions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type OptionRequest struct {
	Label   string `json:"label" validate:"required"`
	Content string `json:"content" validate:"required"`
}

type QuestionRequest struct {
	Type    string          `json:"type" validate:"required,oneof=CHOICE TEXT"`
	Content string          `json:"content" validate:"required,min=1,max=2000"`
	Options []OptionRequest `json:"options" validate:"required_if=Type CHOICE,dive,required"`
}

type BoundQuestion struct {
	Question Question
	Options  []Option
}

type QuestionResponse struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Content string   `json:"content"`
	Options []Option `json:"options"`
}

type AnswerRequest struct {
	SelectedOptionID string `json:"selectedOptionID"`
	TextAnswer       string `json:"textAnswer"`
}

type AnswerResponse struct {
	ID               string `json:"id"`
	QuestionID       string `json:"questionID"`
	SelectedOptionID string `json:"selectedOptionID"`
	TextAnswer       string `json:"textAnswer"`
	CreateAt         string `json:"createAt"`
}

//go:generate mockery --name=Store
type Store interface {
	CreateQuestion(ctx context.Context, arg QuestionRequest) (BoundQuestion, error)
	ListQuestion(ctx context.Context) ([]BoundQuestion, error)
	GetQuestion(ctx context.Context, ID uuid.UUID) (BoundQuestion, error)
	UpdateQuestion(ctx context.Context, ID uuid.UUID, arg QuestionRequest) (BoundQuestion, error)
	DelQuestion(ctx context.Context, ID uuid.UUID) error

	SubmitAnswer(ctx context.Context, arg SubmitAnswerParams) (Answer, error)
	ListAnswer(ctx context.Context, questionID uuid.UUID) ([]Answer, error)
}

type Handler struct {
	logger    *zap.Logger
	validator *validator.Validate
	store     Store
}

func NewHandler(logger *zap.Logger, validator *validator.Validate, store Store) *Handler {
	return &Handler{
		logger:    logger,
		validator: validator,
		store:     store,
	}
}

/**** Questions ****/

func (h *Handler) CreateQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req QuestionRequest
	err := h.DecodeReqBody(w, r, &req)
	if err != nil {
		return
	}

	err = h.ValidateCheck(w, &req)
	if err != nil {
		return
	}

	newQuestion, err := h.store.CreateQuestion(ctx, QuestionRequest{
		Type:    req.Type,
		Content: req.Content,
		Options: req.Options,
	})
	if err != nil {
		h.logger.Error("failed to create question", zap.Error(err))
		http.Error(w, "server failed to create question", http.StatusInternalServerError)
		return
	}

	resp := QuestionResponse{
		ID:      newQuestion.Question.ID.String(),
		Type:    newQuestion.Question.Type,
		Content: newQuestion.Question.Content,
		Options: newQuestion.Options,
	}

	err = h.WriteResponse(w, "application/json", http.StatusCreated, resp)
	if err != nil {
		return
	}
}

// ListQuestion - Get all questions.sql
func (h *Handler) ListQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	getQuestions, err := h.store.ListQuestion(ctx)
	if err != nil {
		h.logger.Error("failed to get questions.sql", zap.Error(err))
		http.Error(w, "failed to get questions.sql", http.StatusInternalServerError)
		return
	}

	resp := make([]QuestionResponse, len(getQuestions))
	for i, questions := range getQuestions {
		resp[i] = QuestionResponse{
			ID:      questions.Question.ID.String(),
			Type:    questions.Question.Type,
			Content: questions.Question.Content,
			Options: questions.Options,
		}
	}

	err = h.WriteResponse(w, "application/json", http.StatusOK, resp)
	if err != nil {
		return
	}

}

// GetQuestion - Get single question
func (h *Handler) GetQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	questionID := r.PathValue("id")
	id, err := ParseUUID(questionID)
	if err != nil {
		return
	}

	getQuestion, err := h.store.GetQuestion(ctx, id)
	if err != nil {
		h.logger.Error("failed to get question", zap.Error(err))
		http.Error(w, "failed to get question", http.StatusInternalServerError)
		return
	}

	resp := QuestionResponse{
		ID:      getQuestion.Question.ID.String(),
		Type:    getQuestion.Question.Type,
		Content: getQuestion.Question.Content,
		Options: getQuestion.Options,
	}

	err = h.WriteResponse(w, "application/json", http.StatusOK, resp)
	if err != nil {
		return
	}
}

func (h *Handler) UpdateQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	questionID := r.PathValue("id")
	id, err := ParseUUID(questionID)
	if err != nil {
		return
	}

	var req QuestionRequest
	err = h.DecodeReqBody(w, r, &req)
	if err != nil {
		return
	}

	err = h.ValidateCheck(w, &req)
	if err != nil {
		return
	}

	updateQuestion, err := h.store.UpdateQuestion(ctx, id, QuestionRequest{
		Type:    req.Type,
		Content: req.Content,
		Options: req.Options,
	})
	if err != nil {
		h.logger.Error("failed to update question", zap.Error(err))
		http.Error(w, "failed to update question", http.StatusInternalServerError)
		return
	}

	resp := QuestionResponse{
		ID:      updateQuestion.Question.ID.String(),
		Type:    updateQuestion.Question.Type,
		Content: updateQuestion.Question.Content,
		Options: updateQuestion.Options,
	}

	err = h.WriteResponse(w, "application/json", http.StatusOK, resp)
	if err != nil {
		return
	}
}

func (h *Handler) DelQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	questionID := r.PathValue("id")
	id, err := ParseUUID(questionID)
	if err != nil {
		return
	}

	err = h.store.DelQuestion(ctx, id)
	if err != nil {
		h.logger.Error("failed to delete question", zap.Error(err))
		http.Error(w, "failed to delete question", http.StatusInternalServerError)
		return
	}

	err = h.WriteResponse(w, "application/json", http.StatusNoContent, nil)
	if err != nil {
		return
	}
}

/**** Answers ****/

func (h *Handler) SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	questionID := r.PathValue("id")
	id, err := ParseUUID(questionID)
	if err != nil {
		return
	}

	var req AnswerRequest
	err = h.DecodeReqBody(w, r, &req)
	if err != nil {
		return
	}

	err = h.ValidateCheck(w, &req)
	if err != nil {
		return
	}

	// default to nil
	var selOptionID *uuid.UUID
	if req.SelectedOptionID != "" {
		parseSelOptionID, err := ParseUUID(req.SelectedOptionID)
		if err != nil {
			return
		}
		selOptionID = &parseSelOptionID
	}

	newAnswer, err := h.store.SubmitAnswer(ctx, SubmitAnswerParams{
		QuestionID:       id,
		SelectedOptionID: selOptionID,
		TextAnswer:       req.TextAnswer,
	})
	if err != nil {
		h.logger.Error("failed to create answer", zap.Error(err))
		http.Error(w, "failed to create answer", http.StatusInternalServerError)
		return
	}

	resp := AnswerResponse{
		ID:               newAnswer.ID.String(),
		QuestionID:       newAnswer.QuestionID.String(),
		SelectedOptionID: req.SelectedOptionID, // trick to avoid dereference of null pointer
		TextAnswer:       newAnswer.TextAnswer,
		CreateAt:         newAnswer.CreatedAt.Time.String(),
	}

	err = h.WriteResponse(w, "application/json", http.StatusCreated, resp)
	if err != nil {
		return
	}
}

func (h *Handler) ListAnswers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	questionID := r.PathValue("id")
	id, err := ParseUUID(questionID)
	if err != nil {
		return
	}

	getAnswer, err := h.store.ListAnswer(ctx, id)
	if err != nil {
		h.logger.Error("failed to get answer", zap.Error(err))
		http.Error(w, "failed to get answer", http.StatusInternalServerError)
		return
	}

	// handle the SelectedOptionID carefully, it's a pointer
	var selOptionID string
	resp := make([]AnswerResponse, len(getAnswer))
	for i, answer := range getAnswer {
		if answer.SelectedOptionID == nil {
			selOptionID = ""
		} else {
			selOptionID = answer.SelectedOptionID.String()
		}
		resp[i] = AnswerResponse{
			ID:               answer.ID.String(),
			QuestionID:       answer.QuestionID.String(),
			SelectedOptionID: selOptionID,
			TextAnswer:       answer.TextAnswer,
			CreateAt:         answer.CreatedAt.Time.String(),
		}
	}

	err = h.WriteResponse(w, "application/json", http.StatusOK, resp)
	if err != nil {
		return
	}
}

/**** Helper Functions ****/

func (h *Handler) DecodeReqBody(w http.ResponseWriter, r *http.Request, req interface{}) error {
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		h.logger.Error("failed to decode request body", zap.Error(err))
		http.Error(w, "invalid request body", http.StatusBadRequest)
	}
	return err
}

func (h *Handler) ValidateCheck(w http.ResponseWriter, req interface{}) error {
	err := h.validator.Struct(req)
	if err != nil {
		h.logger.Error("request validation check failed", zap.Error(err))
		http.Error(w, "request validation check failed", http.StatusBadRequest)
	}
	return err
}

func (h *Handler) WriteResponse(w http.ResponseWriter, contentType string, statusCode int, resp any) error {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)

	if resp == nil {
		return nil
	}

	err := json.NewEncoder(w).Encode(&resp)
	if err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
	return err
}

func ParseUUID(value string) (uuid.UUID, error) {
	parsedUUID, err := uuid.Parse(value)
	if err != nil {
		return parsedUUID, fmt.Errorf("failed to parse UUID: %w", err)
	}
	return parsedUUID, nil
}
