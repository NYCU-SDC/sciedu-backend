package chat

import (
	"context"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	logutil "github.com/NYCU-SDC/summer/pkg/log"
	problemutil "github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"net/http"
)

type Store interface {
	CreateChat(ctx context.Context) (uuid.UUID, error)
	GetChat(ctx context.Context, chatID uuid.UUID) ([]MessageReturn, error)
}

type Handler struct {
	logger        *zap.Logger
	problemWriter *problemutil.HttpWriter
	store         Store
}

func NewHandler(store Store, logger *zap.Logger) *Handler {
	return &Handler{
		logger: logger,
		store:  store,
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

	messages, err := h.store.GetChat(ctx, chatID)
	if err != nil {
		h.problemWriter.WriteError(ctx, w, err, logger)
		return
	}

	handlerutil.WriteJSONResponse(w, http.StatusOK, map[string][]MessageReturn{"messages": messages})
}
