package chat_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sciedu-backend/internal/chat"
	"testing"
	"time"
)

func TestHTTPProvider_VerifyHTTPClientStreaming_LineByLine(t *testing.T) {
	t.Parallel()

	firstFlushed := make(chan struct{})
	allowSecond := make(chan struct{})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("upstream does not support flushing")
		}

		w.Header().Set("Content-Type", "text/event-Stream")
		w.WriteHeader(http.StatusOK)

		// First event, flush immediately
		_, _ = io.WriteString(w, "data: A\n\n")
		fl.Flush()
		close(firstFlushed)

		// Block until test allows second event
		<-allowSecond

		_, _ = io.WriteString(w, "data: B\n\n")
		fl.Flush()

		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer upstream.Close()

	p := chat.NewProvider(upstream.URL, &http.Client{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := chat.CreateChatCompletionRequest{
		Messages: []chat.ChatMessage{{Role: chat.ChatRoleUser, Content: "hi"}},
		Stream:   true,
	}

	chunks, errs := p.StreamChat(ctx, req)

	// Ensure first chunk has been flushed by upstream
	select {
	case <-firstFlushed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("upstream did not flush first chunk in time")
	}

	// Key assertion: must receive "A" before allowing second chunk
	select {
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case got := <-chunks:
		if got.Delta != "A" || got.IsFinished {
			t.Fatalf("unexpected first chunk: %+v", got)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("did not receive first chunk quickly; likely buffering entire response body")
	}

	close(allowSecond)

	// Receive second chunk
	select {
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case got := <-chunks:
		if got.Delta != "B" || got.IsFinished {
			t.Fatalf("unexpected second chunk: %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive second chunk")
	}

	// Receive DONE
	select {
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case got := <-chunks:
		if !got.IsFinished {
			t.Fatalf("expected finished chunk, got: %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("did not receive finished chunk")
	}
}

func TestHTTPProvider_VerifyContextPropagation_CancelUpstreamOnClientDisconnect(t *testing.T) {
	t.Parallel()

	upstreamCancelled := make(chan struct{})
	upstreamStarted := make(chan struct{})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("upstream does not support flushing")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		close(upstreamStarted)

		// Emit one chunk so downstream sees activity
		_, _ = io.WriteString(w, "data: Hello\n\n")
		fl.Flush()

		<-r.Context().Done()
		close(upstreamCancelled)
	}))
	defer upstream.Close()

	p := chat.NewProvider(upstream.URL, &http.Client{}, nil)

	ctx, cancel := context.WithCancel(context.Background())

	req := chat.CreateChatCompletionRequest{
		Messages: []chat.ChatMessage{{Role: chat.ChatRoleUser, Content: "hi"}},
		Stream:   true,
	}

	chunks, errs := p.StreamChat(ctx, req)

	// Ensure upstream started
	select {
	case <-upstreamStarted:
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatal("upstream did not start in time")
	}

	// Read at least one chunk to ensure request is active and body is being read
	select {
	case err := <-errs:
		cancel()
		t.Fatalf("unexpected error: %v", err)
	case <-chunks:
		// ok
	case <-time.After(500 * time.Millisecond):
		cancel()
		t.Fatal("did not receive first chunk in time")
	}

	// Simulate downstream disconnect by cancelling ctx
	cancel()

	// Verify upstream observes cancellation shortly
	select {
	case <-upstreamCancelled:
		// PASS
	case <-time.After(800 * time.Millisecond):
		t.Fatal("expected upstream request to be cancelled after ctx cancel")
	}
}
