package chat

import (
	"context"
	"encoding/json"
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
		require.Equal(t, "/agents", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body AgentsRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "default-agents", body.Preset)
		require.Equal(t, "chat-id", body.Session)
		require.Equal(t, "user-id", body.User)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"part_start\",\"index\":0,\"part\":{\"type\":\"text\",\"id\":\"p0\",\"agent\":\"teacher\"}}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"delta\",\"index\":0,\"delta\":\"hello\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"done\",\"finishReason\":\"stop\",\"status\":\"completed\"}\n\n")
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client(), nil)
	chunks, errs := provider.Stream(context.Background(), AgentsRequest{
		Messages: []ChatMessage{{Role: MessageRoleUser, Content: "hello"}},
		Preset:   "default-agents",
		Session:  "chat-id",
		User:     "user-id",
	})

	first := receiveStreamChunk(t, chunks)
	require.NotNil(t, first.Agentic)
	require.Equal(t, AgentEventPartStart, first.Agentic.Type)

	second := receiveStreamChunk(t, chunks)
	require.NotNil(t, second.Agentic)
	require.Equal(t, "hello", second.Agentic.Delta)

	third := receiveStreamChunk(t, chunks)
	require.True(t, third.Terminal())

	require.NoError(t, receiveErr(t, errs))
}

func TestProviderStreamTreatsEOFBeforeFinishAsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"delta\",\"index\":0,\"delta\":\"partial\"}\n\n")
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client(), nil)
	chunks, errs := provider.Stream(context.Background(), AgentsRequest{
		Messages: []ChatMessage{{Role: MessageRoleUser, Content: "hello"}},
	})

	first := receiveStreamChunk(t, chunks)
	require.Equal(t, "partial", first.Agentic.Delta)

	err := receiveErr(t, errs)
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream closed before finish")
}

func TestProviderStreamSupportsLegacyChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"delta\":\"hello\",\"isFinished\":false}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"delta\":\"\",\"isFinished\":true}\n\n")
	}))
	defer server.Close()

	provider := NewProvider(server.URL, server.Client(), nil)
	chunks, errs := provider.Stream(t.Context(), AgentsRequest{Messages: []ChatMessage{{Role: MessageRoleUser, Content: "hello"}}})

	first := receiveStreamChunk(t, chunks)
	require.Equal(t, &StreamDelta{Delta: "hello"}, first.Legacy)
	require.True(t, receiveStreamChunk(t, chunks).Terminal())
	require.NoError(t, receiveErr(t, errs))
}

func TestParseSSEEventDataPreservesTypedPartPayloads(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		wantType AgentEventType
		terminal bool
	}{
		{
			name:     "tool call arguments",
			payload:  `{"type":"part_end","index":0,"part":{"type":"tool_call","id":"p0","agent":"teacher","tool_call_id":"call-1","name":"rag_search","arguments":{"query":"光合作用"}}}`,
			wantType: AgentEventPartEnd,
		},
		{
			name:     "tool result string content",
			payload:  `{"type":"part_end","index":1,"part":{"type":"tool_result","id":"p1","agent":"teacher","tool_call_id":"call-1","status":"ok","content":"result"}}`,
			wantType: AgentEventPartEnd,
		},
		{
			name:     "done",
			payload:  `{"type":"done","finishReason":"stop","status":"completed"}`,
			wantType: AgentEventDone,
			terminal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunk, terminal, err := parseSSEEventData(tt.payload)
			require.NoError(t, err)
			require.Equal(t, tt.terminal, terminal)
			require.NotNil(t, chunk.Agentic)
			require.Equal(t, tt.wantType, chunk.Agentic.Type)

			encoded, err := json.Marshal(chunk)
			require.NoError(t, err)
			require.JSONEq(t, tt.payload, string(encoded))
		})
	}
}

func receiveStreamChunk(t *testing.T, chunks <-chan StreamChunk) StreamChunk {
	t.Helper()

	select {
	case chunk, ok := <-chunks:
		require.True(t, ok)
		return chunk
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream chunk")
		return StreamChunk{}
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
