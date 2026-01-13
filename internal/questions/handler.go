package questions

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"
)

type ReqOption struct {
	Label   string `json:"label" validate:"required"`
	Content string `json:"content" validate:"required"`
}

type Option struct {
	ID      string
	Label   string
	Content string
}

type QuestionRequest struct {
	Type    string      `json:"type" validate:"required,oneof=CHOICE TEXT"`
	Content string      `json:"content" validate:"required,min=1,max=2000"`
	Options []ReqOption `json:"options" validate:"required_if=Type CHOICE,dive,required"`
}

// after connected to the database, Question struct in service will be modified
// hence, I left a similar struct here in order to simplify our work later
type QuestionResponse struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Content string   `json:"content"`
	Options []Option `json:"options"`
}

type AnswerRequest struct {
	SelectedOptionID int    `json:"selectedOptionID"`
	TextAnswer       string `json:"textAnswer"`
}

type AnswerResponse struct {
	ID               string `json:"id"`
	QuestionID       string `json:"questionID"`
	SelectedOptionID int    `json:"selectedOptionID"`
	TextAnswer       string `json:"textAnswer"`
	CreateAt         string `json:"createAt"`
}

type Store interface {
	CreateQuestion(ctx context.Context, arg ReqQuestion) (Question, error)
	ListQuestion(ctx context.Context) ([]Question, error)
	GetQuestion(ctx context.Context, ID string) (Question, error)
	UpdateQuestion(ctx context.Context, ID string, arg ReqQuestion) (Question, error)
	DelQuestion(ctx context.Context, ID string) error

	CreateAnswer(ctx context.Context, questionID string, arg ReqAnswer) (Answer, error)
	GetAnswer(ctx context.Context, questionID string) (Answer, error)
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

	newQuestion, err := h.store.CreateQuestion(ctx, ReqQuestion{
		Type:       req.Type,
		Content:    req.Content,
		ReqOptions: req.Options,
	})
	if err != nil {
		h.logger.Error("failed to create question", zap.Error(err))
		http.Error(w, "server failed to create question", http.StatusInternalServerError)
		return
	}

	// modified type after connected to the database,
	// since for mock data, I choose string type instead of pgtype.UUID type
	// ex: ewQuestion.ID => newQuestion.ID.String()
	resp := QuestionResponse{
		ID:      newQuestion.ID,
		Type:    newQuestion.Type,
		Content: newQuestion.Content,
		Options: newQuestion.Options,
	}

	err = h.WriteResponse(w, "application/json", http.StatusCreated, resp)
	if err != nil {
		return
	}
}

// ListQuestion - Get all questions
func (h *Handler) ListQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	getQuestions, err := h.store.ListQuestion(ctx)
	if err != nil {
		h.logger.Error("failed to get questions", zap.Error(err))
		http.Error(w, "failed to get questions", http.StatusInternalServerError)
		return
	}

	resp := make([]QuestionResponse, len(getQuestions))
	for i, questions := range getQuestions {
		resp[i] = QuestionResponse{
			ID:      questions.ID,
			Type:    questions.Type,
			Content: questions.Content,
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

	// UUID check logic
	// struct we used here is mock, thus we skip this part
	// MUST handle this properly after connected to the database

	getQuestion, err := h.store.GetQuestion(ctx, questionID)
	if err != nil {
		h.logger.Error("failed to get question", zap.Error(err))
		http.Error(w, "failed to get question", http.StatusInternalServerError)
		return
	}

	resp := QuestionResponse{
		ID:      getQuestion.ID,
		Type:    getQuestion.Type,
		Content: getQuestion.Content,
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

	var req QuestionRequest
	err := h.DecodeReqBody(w, r, &req)
	if err != nil {
		return
	}

	err = h.ValidateCheck(w, &req)
	if err != nil {
		return
	}

	// UUID check logic

	updateQuestion, err := h.store.UpdateQuestion(ctx, questionID, ReqQuestion{
		Type:       req.Type,
		Content:    req.Content,
		ReqOptions: req.Options,
	})
	if err != nil {
		h.logger.Error("failed to update question", zap.Error(err))
		http.Error(w, "failed to update question", http.StatusInternalServerError)
		return
	}

	resp := QuestionResponse{
		ID:      updateQuestion.ID,
		Type:    updateQuestion.Type,
		Content: updateQuestion.Content,
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

	// UUID check logic
	// struct we used here is mock, thus we skip this part
	// MUST handle this properly after connected to the database

	err := h.store.DelQuestion(ctx, questionID)
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

func (h *Handler) CreateAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	questionID := r.PathValue("id")

	var req AnswerRequest
	err := h.DecodeReqBody(w, r, &req)
	if err != nil {
		return
	}

	err = h.ValidateCheck(w, &req)
	if err != nil {
		return
	}

	newAnswer, err := h.store.CreateAnswer(ctx, questionID, ReqAnswer{
		SelectedOptionID: req.SelectedOptionID,
		TextAnswer:       req.TextAnswer,
	})
	if err != nil {
		h.logger.Error("failed to create answer", zap.Error(err))
		http.Error(w, "failed to create answer", http.StatusInternalServerError)
		return
	}

	resp := AnswerResponse{
		ID:               newAnswer.ID,
		QuestionID:       newAnswer.QuestionID,
		SelectedOptionID: newAnswer.SelectedOptionID,
		TextAnswer:       newAnswer.TextAnswer,
		CreateAt:         newAnswer.CreateAt,
	}

	err = h.WriteResponse(w, "application/json", http.StatusCreated, resp)
	if err != nil {
		return
	}
}

func (h *Handler) GetAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	questionID := r.PathValue("id")

	// UUID check logic
	// struct we used here is mock, thus we skip this part
	// MUST handle this properly after connected to the database

	getAnswer, err := h.store.GetAnswer(ctx, questionID)
	if err != nil {
		h.logger.Error("failed to get answer", zap.Error(err))
		http.Error(w, "failed to get answer", http.StatusInternalServerError)
		return
	}

	resp := AnswerResponse{
		ID:               getAnswer.ID,
		QuestionID:       getAnswer.QuestionID,
		SelectedOptionID: getAnswer.SelectedOptionID,
		TextAnswer:       getAnswer.TextAnswer,
		CreateAt:         getAnswer.CreateAt,
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

	err := json.NewEncoder(w).Encode(&resp)
	if err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
	return err
}
