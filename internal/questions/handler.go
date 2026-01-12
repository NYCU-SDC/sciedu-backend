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

type Request struct {
	Type    string      `json:"type" validate:"required,oneof=CHOICE TEXT"`
	Content string      `json:"content" validate:"required,min=1,max=2000"`
	Options []ReqOption `json:"options" validate:"required_if=Type CHOICE,dive"`
}

type Response struct {
	ID      string
	Type    string
	Content string
	Options []Option
}

type Store interface {
	Create(ctx context.Context, arg CreateParam) (Question, error)
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

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req Request
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		h.logger.Error("failed to decode request body", zap.Error(err))
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	err = h.validator.Struct(req)
	if err != nil {
		h.logger.Error("request validation check failed", zap.Error(err))
		http.Error(w, "request validation check failed", http.StatusBadRequest)
		return
	}

	newQuestion, err := h.store.Create(ctx, CreateParam{
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
	// since for mock date, I choose string type instead of pgtype.UUID type
	// ex: ewQuestion.ID => newQuestion.ID.String()
	resp := Response{
		ID:      newQuestion.ID,
		Type:    newQuestion.Type,
		Content: newQuestion.Content,
		Options: newQuestion.Options,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(&resp)
	if err != nil {
		h.logger.Error("failed to encode response", zap.Error(err))
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}
