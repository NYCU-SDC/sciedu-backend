package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

type Store interface {
	StreamChat(ctx context.Context, req CreateChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error)
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

type RFC9457 struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	Status   int    `json:"status,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

type validationError struct {
	status  int
	problem RFC9457
}

func (h *Handler) StreamChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, r, http.StatusMethodNotAllowed, RFC9457{
			Type:   "about:blank",
			Title:  "Method Not Allowed",
			Status: http.StatusMethodNotAllowed,
			Detail: "Only POST is supported for this endpoint.",
		})
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, RFC9457{
			Type:   "about:blank",
			Title:  "Unsupported Media Type",
			Status: http.StatusUnsupportedMediaType,
			Detail: "Content-Type must be application/json.",
		})
		return
	}

	req, vErr := decodeAndValidateRequest(r)
	if vErr != nil {
		writeProblem(w, r, vErr.status, vErr.problem)
		return
	}

	// Service expects streaming
	req.Stream = true

	// ---- SSE protocol setup (headers must be sent once) ----
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, RFC9457{
			Type:   "about:blank",
			Title:  "Internal Server Error",
			Status: http.StatusInternalServerError,
			Detail: "Streaming unsupported by server.",
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	// Helpful behind proxies (e.g., nginx)
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Streaming phase
	ctx := r.Context()
	chunks, errs := h.store.StreamChat(ctx, req)

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("stream cancelled: %v", zap.Error(ctx.Err()))
			return

		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				// Close
				h.logger.Error("stream error: %v", zap.Error(err))

				return
			}

		case chunk, ok := <-chunks:
			if !ok {
				// normal completion
				return
			}
			if err := writeSSEData(w, flusher, chunk); err != nil {
				h.logger.Error("write SSE data failed: %v", zap.Error(err))
				return
			}
		}
	}
}

func writeSSEData(w http.ResponseWriter, flusher http.Flusher, chunk ChatCompletionChunk) error {
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

func decodeAndValidateRequest(r *http.Request) (CreateChatCompletionRequest, *validationError) {
	// protect against large bodies
	const maxBody = 1 << 20 // 1 MiB
	body := io.LimitReader(r.Body, maxBody)

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()

	var req CreateChatCompletionRequest
	if err := dec.Decode(&req); err != nil {
		// Include common JSON errors (syntax, unknown fields, etc.)
		return CreateChatCompletionRequest{}, &validationError{
			status: http.StatusBadRequest,
			problem: RFC9457{
				Type:   "https://example.com/problems/invalid-json",
				Title:  "Invalid JSON",
				Status: http.StatusBadRequest,
				Detail: sanitizeJSONError(err),
			},
		}
	}

	// Ensure no trailing tokens
	if dec.More() {
		return CreateChatCompletionRequest{}, &validationError{
			status: http.StatusBadRequest,
			problem: RFC9457{
				Type:   "https://example.com/problems/invalid-json",
				Title:  "Invalid JSON",
				Status: http.StatusBadRequest,
				Detail: "Request body must contain a single JSON object.",
			},
		}
	}

	// TSP-like validation: messages required, each message role/content required
	fieldErrs := map[string]string{}

	if len(req.Messages) == 0 {
		fieldErrs["messages"] = "messages is required and must be a non-empty array."
	} else {
		for i, m := range req.Messages {
			if strings.TrimSpace(m.Content) == "" {
				fieldErrs[msgField(i, "content")] = "content is required."
			}
			if strings.TrimSpace(m.Role.User) == "" || strings.TrimSpace(m.Role.Assistant) == "" || strings.TrimSpace(m.Role.System) == "" {
				fieldErrs[msgField(i, "role")] = "role must specify one of 'user', 'assistant', or 'system'."
			}
		}
	}

	if len(fieldErrs) > 0 {
		var details string
		for field, msg := range fieldErrs {
			details = details + field + ": " + msg + "\n"
		}
		return CreateChatCompletionRequest{}, &validationError{
			status: http.StatusBadRequest,
			problem: RFC9457{
				Type:   "https://example.com/problems/validation-error",
				Title:  "Validation Error",
				Status: http.StatusBadRequest,
				Detail: details,
			},
		}
	}

	return req, nil
}

func msgField(i int, name string) string {
	return "messages[" + strconv.Itoa(i) + "]." + name
}

func sanitizeJSONError(err error) string {
	// Avoid leaking internal details; keep error user-actionable.
	// DisallowUnknownFields often returns: "json: unknown field ..."
	msg := err.Error()
	if strings.Contains(msg, "unknown field") {
		return msg
	}
	// Syntax errors / EOF are acceptable to report as-is at this granularity
	return msg
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, p RFC9457) {
	p.Status = status
	if p.Title == "" {
		p.Title = http.StatusText(status)
	}
	if p.Type == "" {
		p.Type = "about:blank"
	}
	p.Instance = r.URL.Path

	b, _ := json.Marshal(p)
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
