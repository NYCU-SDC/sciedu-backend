package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/NYCU-SDC/summer/pkg/problem"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"sciedu-backend/internal/respond"
)

type Handler struct {
	service *Service
	logger  *zap.Logger
}

func NewHandler(service *Service, logger *zap.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/chat", h.createChat)
	mux.HandleFunc("/api/chat/", h.route)
}

func (h *Handler) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/chat/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch {
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "stream":
		h.stream(w, r, parts[1])
	case len(parts) == 2 && parts[1] == "messages":
		switch r.Method {
		case http.MethodGet:
			h.listMessages(w, r, parts[0])
		case http.MethodPost:
			h.sendMessage(w, r, parts[0])
		default:
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) createChat(w http.ResponseWriter, r *http.Request) {
	id, err := h.service.CreateChat(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, CreateChatResponse{ChatID: id})
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request, rawChatID string) {
	chatID, ok := h.parseUUID(w, r, rawChatID)
	if !ok {
		return
	}
	messages, err := h.service.ListMessages(r.Context(), chatID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusOK, MessagesResponse{Messages: messages})
}

func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request, rawChatID string) {
	chatID, ok := h.parseUUID(w, r, rawChatID)
	if !ok {
		return
	}
	var req SendMessageRequest
	if err := respond.DecodeJSON(r, &req); err != nil {
		h.writeError(w, r, err)
		return
	}
	message, replyID, err := h.service.SendMessage(r.Context(), chatID, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, SendMessageResponse{Message: message, ReplyMessageID: replyID})
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request, rawMessageID string) {
	messageID, ok := h.parseUUID(w, r, rawMessageID)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, r, fmt.Errorf("%w: streaming is unsupported", ErrInvalidChat))
		return
	}

	initialContent, events, unsubscribe, err := h.service.SubscribeStream(r.Context(), messageID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if initialContent != "" {
		if err := h.writeStreamEvent(w, StreamEvent{Type: StreamEventDelta, Content: initialContent}); err != nil {
			h.logger.Error("failed to write initial stream chunk", zap.Error(err))
			return
		}
		flusher.Flush()
	}

	for event := range events {
		if err := h.writeStreamEvent(w, event); err != nil {
			h.logger.Error("failed to write stream event", zap.Error(err))
			return
		}
		flusher.Flush()
	}
}

func (h *Handler) writeStreamEvent(w http.ResponseWriter, event StreamEvent) error {
	if _, err := fmt.Fprintf(w, "event: %s\n", event.Type); err != nil {
		return err
	}

	var payload []byte
	var err error
	switch event.Type {
	case StreamEventDelta:
		payload, err = json.Marshal(StreamChunk{Content: event.Content})
	case StreamEventError:
		message := "stream failed"
		if event.Err != nil {
			message = event.Err.Error()
		}
		payload, err = json.Marshal(StreamError{Error: message})
	default:
		payload = []byte("{}")
	}
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
	return err
}

func (h *Handler) parseUUID(w http.ResponseWriter, r *http.Request, raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		h.writeError(w, r, handlerutil.ErrInvalidUUID)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	writer := problem.NewWithMapping(func(err error) problem.Problem {
		if errors.Is(err, ErrInvalidChat) {
			return problem.NewValidateProblem(err.Error())
		}
		if errors.Is(err, ErrStreamNotFound) {
			return problem.NewNotFoundProblem(err.Error())
		}
		return problem.Problem{}
	})
	writer.WriteError(r.Context(), w, err, h.logger)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, payload any) {
	if err := respond.JSON(w, status, payload); err != nil {
		h.logger.Error("failed to write response", zap.Error(err))
	}
}
