package chatPrototype

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestHTTPSSEClientStreaming verifies that the client reads data line-by-line
// from a continuous HTTP Server output, rather than buffering the entire response body.
func TestHTTPSSEClientStreaming(t *testing.T) {
	logger := zap.NewNop()

	// Track when each line is written by the server and received by the client
	var writeTimes []time.Time
	var receiveTimes []time.Time
	var mu sync.Mutex

	// Create a test server that sends SSE events with delays
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "ResponseWriter must support flushing")

		// Send 5 events with 50ms delay between each
		for i := 1; i <= 5; i++ {
			mu.Lock()
			writeTimes = append(writeTimes, time.Now())
			mu.Unlock()

			_, err := fmt.Fprintf(w, "data: message_%d\n\n", i)
			if err != nil {
				return
			}
			flusher.Flush()

			// Delay before next message (except after the last one)
			if i < 5 {
				time.Sleep(50 * time.Millisecond)
			}
		}

		// Send the done marker
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := NewHTTPSSEClient(logger, server.Client(), server.URL)
	ctx := context.Background()

	contents, errs := client.StreamChat(ctx)

	// Collect all messages
	var messages []string
	var streamErr error

	go func() {
		for err := range errs {
			streamErr = err
		}
	}()

	for content := range contents {
		mu.Lock()
		receiveTimes = append(receiveTimes, time.Now())
		mu.Unlock()

		messages = append(messages, content)
	}

	// Verify no errors occurred
	require.NoError(t, streamErr)

	// Verify we received all 5 messages
	require.Len(t, messages, 5, "Should receive exactly 5 messages")
	for i := 1; i <= 5; i++ {
		assert.Equal(t, fmt.Sprintf("message_%d", i), messages[i-1])
	}

	// Critical verification: Each message should be received shortly after it's written,
	// proving that we're NOT buffering the entire response before processing.
	// If buffering occurred, all receive times would be clustered together at the end.
	mu.Lock()
	defer mu.Unlock()

	require.Len(t, writeTimes, 5, "Should have 5 write timestamps")
	require.Len(t, receiveTimes, 5, "Should have 5 receive timestamps")

	// Verify that messages are received incrementally (within reasonable time after writing)
	for i := 0; i < 5; i++ {
		timeDiff := receiveTimes[i].Sub(writeTimes[i])
		assert.Less(t, timeDiff, 100*time.Millisecond,
			"Message %d should be received within 100ms of being written (got %v), proving line-by-line streaming",
			i+1, timeDiff)
	}

	// Additional verification: The time between first and last receive should be >= 150ms
	// (because of the 50ms delays between writes), proving incremental processing
	totalReceiveDuration := receiveTimes[4].Sub(receiveTimes[0])
	assert.GreaterOrEqual(t, totalReceiveDuration, 150*time.Millisecond,
		"Messages should be received incrementally over time, not all at once")

	t.Logf("✓ Verified line-by-line streaming: messages received incrementally over %v", totalReceiveDuration)
}

// TestContextPropagation verifies that when the API client disconnects
// (context is cancelled), the outgoing request to the upstream service is automatically
// cancelled via the Context mechanism.
func TestContextPropagation(t *testing.T) {
	logger := zap.NewNop()

	// Track whether the server detected the client disconnection
	var serverDetectedDisconnect bool
	var serverMu sync.Mutex
	serverStarted := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		require.True(t, ok)

		// Signal that server started processing
		close(serverStarted)

		// Try to send many messages
		for i := 1; i <= 100; i++ {
			// Check if client disconnected
			select {
			case <-r.Context().Done():
				serverMu.Lock()
				serverDetectedDisconnect = true
				serverMu.Unlock()
				return
			default:
			}

			_, err := fmt.Fprintf(w, "data: message_%d\n\n", i)
			if err != nil {
				// Write failed - client likely disconnected
				serverMu.Lock()
				serverDetectedDisconnect = true
				serverMu.Unlock()
				return
			}
			flusher.Flush()

			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer server.Close()

	client := NewHTTPSSEClient(logger, server.Client(), server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	contents, errs := client.StreamChat(ctx)

	// Collect messages until we cancel
	var messages []string
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for err := range errs {
			// Errors are expected after cancellation
			t.Logf("Error channel received: %v", err)
		}
	}()

	// Wait for server to start
	<-serverStarted

	// Read a few messages to ensure streaming has started
	messageCount := 0
	for content := range contents {
		messages = append(messages, content)
		messageCount++

		// After receiving 3 messages, cancel the context
		if messageCount == 3 {
			t.Logf("Received %d messages, now cancelling context", messageCount)
			cancel()

			// Give a short time for the cancellation to propagate
			time.Sleep(100 * time.Millisecond)
			break
		}
	}

	// Continue draining the channels to allow goroutines to finish
	for range contents {
		// Drain any remaining messages
	}

	wg.Wait()

	// Verify we received the initial messages before cancellation
	assert.GreaterOrEqual(t, len(messages), 3, "Should have received at least 3 messages before cancellation")
	assert.Less(t, len(messages), 100, "Should NOT have received all 100 messages after cancellation")

	// Critical verification: Server should have detected the disconnection
	serverMu.Lock()
	detected := serverDetectedDisconnect
	serverMu.Unlock()

	assert.True(t, detected, "Server MUST detect client disconnection via context cancellation")

	t.Logf("✓ Verified context propagation: client received %d messages before disconnect, server detected cancellation", len(messages))
}
