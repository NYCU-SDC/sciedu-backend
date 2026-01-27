package mockLLM

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sciedu-backend/internal/chat"
	"sync"
	"time"
)

// MockLLM is a controllable upstream LLM server for tests.
// It exposes its own URL and can stream SSE responses.
// It also captures the last request payload for assertions.
type MockLLM struct {
	srv *httptest.Server

	mu       sync.Mutex
	lastReq  chat.CreateChatCompletionRequest
	reqCount int

	// Controls
	Every time.Duration
	Parts []string

	// Signals
	Started   chan struct{}
	Cancelled chan struct{}
}

func NewMockLLM() *MockLLM {
	m := &MockLLM{
		Every:     1000 * time.Millisecond,
		Parts:     []string{"This ", "is ", "a ", "mock ", "LLM ", "response."},
		Started:   make(chan struct{}),
		Cancelled: make(chan struct{}),
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.Handle))
	return m
}

func (m *MockLLM) Close() {
	if m.srv != nil {
		m.srv.Close()
	}
}

func (m *MockLLM) URL() string {
	return m.srv.URL
}

func (m *MockLLM) LastRequest() (chat.CreateChatCompletionRequest, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastReq, m.reqCount
}

func (m *MockLLM) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Capture request body
	var req chat.CreateChatCompletionRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("bad json: %v", err), http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.lastReq = req
	m.reqCount++
	m.mu.Unlock()

	// SSE setup
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	select {
	case <-m.Started:
		// already closed
	default:
		close(m.Started)
	}

	// Stream chunks as JSON ChatCompletionChunk
	ticker := time.NewTicker(m.Every)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-r.Context().Done():
			fmt.Println("MockLLM: Got request context done")
			select {
			case <-m.Cancelled:
			default:
				close(m.Cancelled)
			}
			return

		case <-ticker.C:
			if i >= len(m.Parts) {
				// send finished chunk (JSON)
				_ = writeSSEJSON(w, fl, chat.ChatCompletionChunk{Delta: "", IsFinished: true})
				return
			}
			_ = writeSSEJSON(w, fl, chat.ChatCompletionChunk{Delta: m.Parts[i], IsFinished: false})
			i++
		}
	}
}

func writeSSEJSON(w http.ResponseWriter, fl http.Flusher, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "data: "); err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n\n"); err != nil {
		return err
	}
	fl.Flush()
	return nil
}
