package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProviderStreamParsesSSEUntilFinish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/chat", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"delta\":\"hello\",\"isFinished\":false}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"delta\":\"\",\"isFinished\":true}\n\n")
	}))
	defer server.Close()

	provider := NewProvider(server.URL+"/chat", server.Client(), nil)
	chunks, errs := provider.Stream(context.Background(), CreateChatCompletionRequest{
		Messages: []ChatMessage{{Role: MessageRoleUser, Content: "hello"}},
	})

	first := receiveChunk(t, chunks)
	require.Equal(t, StreamDelta{Delta: "hello", IsFinished: false}, first)

	second := receiveChunk(t, chunks)
	require.Equal(t, StreamDelta{Delta: "", IsFinished: true}, second)

	require.NoError(t, receiveErr(t, errs))
}

func TestProviderStreamTreatsEOFBeforeFinishAsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"delta\":\"partial\",\"isFinished\":false}\n\n")
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client(), nil)
	chunks, errs := provider.Stream(context.Background(), CreateChatCompletionRequest{
		Messages: []ChatMessage{{Role: MessageRoleUser, Content: "hello"}},
	})

	first := receiveChunk(t, chunks)
	require.Equal(t, StreamDelta{Delta: "partial", IsFinished: false}, first)

	err := receiveErr(t, errs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream closed before finish")
}

func receiveChunk(t *testing.T, chunks <-chan StreamDelta) StreamDelta {
	t.Helper()

	select {
	case chunk, ok := <-chunks:
		require.True(t, ok)
		return chunk
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream chunk")
		return StreamDelta{}
	}
}

func receiveErr(t *testing.T, errs <-chan error) error {
	t.Helper()

	select {
	case err, ok := <-errs:
		if !ok {
			return nil
		}
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream error")
		return nil
	}
}
