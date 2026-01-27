package chatPrototype

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

type Store interface {
	StreamChat(ctx context.Context) (<-chan string, <-chan error)
}

type Handler struct {
	logger *zap.Logger
	store  Store
}

func NewHandler(logger *zap.Logger, store Store) *Handler {
	return &Handler{
		logger: logger,
		store:  store,
	}
}

func (h *Handler) Downstream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	h.logger.Info("Started streaming chatPrototype response")

	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	contents, errs := h.store.StreamChat(ctx)
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("Client closed the connection")
			return
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				h.logger.Error("Error streaming chatPrototype response", zap.Error(err))
				return
			}
		case content, ok := <-contents:
			if !ok {
				h.logger.Info("Finished streaming chatPrototype response")
				return
			}
			_, err := w.Write([]byte("data: " + content + "\n\n"))
			if err != nil {
				h.logger.Error("Error writing content to response", zap.Error(err))
				return
			}
			flusher.Flush()
			h.logger.Debug("Streamed chatPrototype response", zap.String("content", content))
		}

	}
}
