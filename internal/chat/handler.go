package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sciedu-backend/internal/middleware"
)

type Store interface {
	CreateChat(ctx context.Context, userID uuid.UUID) (uuid.UUID, error)
	GetChat(ctx context.Context, chatID uuid.UUID) (Chat, []MessageReturn, error)
	CreateMessage(ctx context.Context, chatID uuid.UUID, content string, previousID uuid.UUID) (CreateMessageReturn, error)
	Stream(ctx context.Context, messageID uuid.UUID) (bool, <-chan StreamDelta, <-chan error, func())
	ValidatePreviousID(ctx context.Context, previousID uuid.UUID, chatID uuid.UUID) error
	ListChats(ctx context.Context, userID uuid.UUID, page, pageSize int32) (ChatPage, error)
	DeleteChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) error
}

type Handler struct {
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
	store         Store
	validator     *validator.Validate
}

type CreateMessageRequest struct {
	Content    string    `json:"content" validate:"required"`
	PreviousID uuid.UUID `json:"previousID,omitempty"`
}

type bodyParseError struct{ err error }

func (e bodyParseError) Error() string { return e.err.Error() }
func (e bodyParseError) Unwrap() error { return e.err }

func NewHandler(store Store, logger *zap.Logger) *Handler {
	return &Handler{
		logger: logger,
		problemWriter: problemutil.NewWithMapping(func(err error) problemutil.Problem {
			var bpe bodyParseError
			if errors.As(err, &bpe) {
				return problemutil.NewBadRequestProblem(bpe.Error())
			}
			return problemutil.Problem{}
		}),
		store:     store,
		validator: validator.New(),
	}
}

func (h *Handler) CreateChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	userID, err := middleware.GetUserID(ctx)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	chatID, err := h.store.CreateChat(ctx, userID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusCreated, map[string]uuid.UUID{"chatID": chatID})
}

func (h *Handler) GetChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	chatID, err := handlerutil.ParseUUID(r.PathValue("chatID"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	status := http.StatusOK

	chat, messages, err := h.store.GetChat(ctx, chatID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	// 200

	if messages == nil {
		messages = []MessageReturn{}
	}

	handlerutil.WriteJSONResponse(w, status, map[string]interface{}{
		"id":        chat.ID,
		"title":     chat.Title,
		"createdAt": chat.CreatedAt,
		"updatedAt": chat.UpdatedAt,
		"messages":  messages,
	})
}

func (h *Handler) CreateMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	chatID, err := handlerutil.ParseUUID(r.PathValue("chatID"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	var req CreateMessageRequest
	if err := handlerutil.ParseAndValidateRequestBody(ctx, h.validator, r, &req); err != nil {
		h.problemWriter.WriteError(ctx, w, bodyParseError{err}, logger)
		return
	}

	err = h.store.ValidatePreviousID(ctx, req.PreviousID, chatID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	message, err := h.store.CreateMessage(ctx, chatID, req.Content, req.PreviousID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusCreated, message)
}

func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	messageID, err := handlerutil.ParseUUID(r.PathValue("messageID"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	ok, chunks, errs, cleanup := h.store.Stream(ctx, messageID)
	if !ok {
		h.problemWriter.WriteError(ctx, w, handlerutil.NewNotFoundError("stream", "messageID", messageID.String(), ""), logger)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.problemWriter.WriteError(ctx, w, fmt.Errorf("streaming unsupported"), logger)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			cleanup()
			return
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			h.problemWriter.WriteError(ctx, w, err, logger)
			return
		case chunk, ok := <-chunks:
			if !ok {
				chunks = nil
				continue
			}

			if err := writeSSEData(w, flusher, chunk); err != nil {
				h.problemWriter.WriteError(ctx, w, err, logger)
				return
			}
			if chunk.IsFinished {
				cleanup()
				return
			}
		}
	}

}

func (h *Handler) ListChats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	userID, err := middleware.GetUserID(ctx)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	page, pageSize, err := parsePaginationParams(r)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	result, err := h.store.ListChats(ctx, userID, page, pageSize)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, result)
}

func (h *Handler) DeleteChat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := logutil.WithContext(ctx, h.logger)

	chatID, err := handlerutil.ParseUUID(r.PathValue("chatID"))
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	userID, err := middleware.GetUserID(ctx)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, handlerutil.ErrUnauthorized, logger)
		return
	}

	err = h.store.DeleteChat(ctx, chatID, userID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusNoContent, nil)
}

func parsePaginationParams(r *http.Request) (int32, int32, error) {
	var page, pageSize int32

	pageRaw := r.URL.Query().Get("page")
	if pageRaw != "" {
		parsed, err := strconv.ParseInt(pageRaw, 10, 32)
		if err != nil || parsed < 1 {
			return 0, 0, bodyParseError{fmt.Errorf("invalid page: %s", pageRaw)}
		}
		page = int32(parsed)
	}

	pageSizeRaw := r.URL.Query().Get("pageSize")
	if pageSizeRaw != "" {
		parsed, err := strconv.ParseInt(pageSizeRaw, 10, 32)
		if err != nil || parsed < 1 {
			return 0, 0, bodyParseError{fmt.Errorf("invalid pageSize: %s", pageSizeRaw)}
		}
		pageSize = int32(parsed)
	}

	return page, pageSize, nil
}

func writeSSEData(w http.ResponseWriter, flusher http.Flusher, chunk StreamDelta) error {
	b, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	// SSE format: data: <json>\n\n
	if _, err := w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
