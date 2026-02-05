package mockLLM

import (
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type MockUpstream struct {
	logger *zap.Logger
	Every  time.Duration
	Parts  []string
}

func NewMockUpstream(logger *zap.Logger) *MockUpstream {
	return &MockUpstream{
		logger: logger,
		Every:  120 * time.Millisecond,
		Parts:  []string{"This ", "is ", "a ", "mock ", "LLM ", "response."},
	}
}

// ServeHTTP streams SSE events like an LLM upstream would.
func (m *MockUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	m.logger.Info("Mock upstream: start streaming")

	ticker := time.NewTicker(m.Every)
	defer ticker.Stop()

	i := 0
	for {
		select {
		case <-ctx.Done():
			// This is the key signal we want to observe when downstream disconnects.
			m.logger.Info("Mock upstream: request cancelled", zap.Error(ctx.Err()))
			return

		case <-ticker.C:
			if i >= len(m.Parts) {
				// mimic DONE
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				m.logger.Info("Mock upstream: done")
				return
			}

			_, err := fmt.Fprintf(w, "data: %s\n\n", m.Parts[i])
			if err != nil {
				m.logger.Info("Mock upstream: client disconnected while writing", zap.Error(err))
				return
			}
			flusher.Flush()
			i++
		}
	}
}
