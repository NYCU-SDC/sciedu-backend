package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Store interface {
	CreateChat(ctx context.Context) (uuid.UUID, error)
	GetChat(ctx context.Context, chatID uuid.UUID) ([]MessageReturn, error)
	CreateMessage(ctx context.Context, chatID uuid.UUID, content string, previousID uuid.UUID) (CreateMessageReturn, error)
	Stream(ctx context.Context, messageID uuid.UUID) (bool, <-chan StreamDelta, <-chan error, func())
	ValidatePreviousID(ctx context.Context, previousID uuid.UUID, chatID uuid.UUID) error
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

	chatID, err := h.store.CreateChat(ctx)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}
	handlerutil.WriteJSONResponse(w, http.StatusOK, map[string]uuid.UUID{"chat_id": chatID})
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

	messages, err := h.store.GetChat(ctx, chatID)
	if err != nil {
		// temporary handling that 502 error
		if errors.Is(err, ErrStatus502) {
			status = http.StatusInternalServerError
		} else {
			h.problemWriter.WriteError(ctx, w, err, logger)
			return
		}

	}

	if messages == nil {
		messages = []MessageReturn{}
	}

	handlerutil.WriteJSONResponse(w, status, map[string][]MessageReturn{"messages": messages})
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

	handlerutil.WriteJSONResponse(w, http.StatusOK, message)
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
