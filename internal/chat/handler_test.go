package chat_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"sciedu-backend/internal/chat"
	"strings"
	"testing"
	"time"
)

type fakeSvc struct {
	chunks chan chat.ChatCompletionChunk
	errs   chan error
}

func (f *fakeSvc) StreamChat(ctx context.Context, req chat.CreateChatCompletionRequest) (<-chan chat.ChatCompletionChunk, <-chan error) {
	return f.chunks, f.errs
}

func TestHandler_InvalidJSON_ReturnsRFC9457(t *testing.T) {
	svc := &fakeSvc{
		chunks: make(chan chat.ChatCompletionChunk),
		errs:   make(chan error, 1),
	}
	h := chat.NewHandler(svc, nil)

	r := httptest.NewRequest(http.MethodPost, "/chat/stream", strings.NewReader(`{bad json`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.StreamChat(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusBadRequest)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type=%q want=application/problem+json", ct)
	}
}

func TestHandler_ValidationError_ReturnsRFC9457WithErrors(t *testing.T) {
	svc := &fakeSvc{
		chunks: make(chan chat.ChatCompletionChunk),
		errs:   make(chan error, 1),
	}
	h := chat.NewHandler(svc, nil)

	body := `{"messages":[],"stream":true}`
	r := httptest.NewRequest(http.MethodPost, "/chat/stream", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.StreamChat(w, r)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusBadRequest)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type=%q want=application/problem+json", ct)
	}
}

func TestHandler_StreamsSSE(t *testing.T) {
	chunks := make(chan chat.ChatCompletionChunk)
	errs := make(chan error, 1)
	svc := &fakeSvc{chunks: chunks, errs: errs}
	h := chat.NewHandler(svc, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/stream", h.StreamChat)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Drive service output
	go func() {
		time.Sleep(50 * time.Millisecond)
		chunks <- chat.ChatCompletionChunk{Delta: "Hel", IsFinished: false}
		chunks <- chat.ChatCompletionChunk{Delta: "lo", IsFinished: false}
		chunks <- chat.ChatCompletionChunk{Delta: "", IsFinished: true}
		close(chunks)
		close(errs)
	}()

	reqBody := `{"messages":[{"role":"user","content":"hi"}],"stream":true}`
	resp, err := http.Post(srv.URL+"/v1/chat/stream", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want=200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type=%q want=text/event-stream", ct)
	}

	br := bufio.NewReader(resp.Body)
	line1, _ := br.ReadString('\n') // "data: ..."
	if !strings.HasPrefix(line1, "data: ") {
		t.Fatalf("expected SSE data line, got=%q", line1)
	}
	blank, _ := br.ReadString('\n') // "\n"
	if blank != "\n" {
		t.Fatalf("expected blank line, got=%q", blank)
	}
}
