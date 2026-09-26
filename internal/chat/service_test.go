package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestChatServiceDeleteChat(t *testing.T) {
	chatID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	dbErr := errors.New("database unavailable")

	tests := []struct {
		name         string
		rowsAffected int64
		deleteErr    error
		wantError    bool
	}{
		{name: "deletes owned chat", rowsAffected: 1},
		{name: "missing or unowned chat is not found", wantError: true},
		{name: "database error is returned", deleteErr: dbErr, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			querier := &fakeChatQuerier{
				deleteRowsAffected: tt.rowsAffected,
				deleteErr:          tt.deleteErr,
			}
			service := NewService(nil, querier, NewStreamHub(), zap.NewNop())

			err := service.DeleteChat(t.Context(), chatID, userID)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, DeleteChatParams{ID: chatID, UserID: userID}, querier.deleteParams)
		})
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "preserves short title", title: "short title", want: "short title"},
		{name: "preserves 255 runes", title: strings.Repeat("好", 255), want: strings.Repeat("好", 255)},
		{name: "truncates ASCII title", title: strings.Repeat("a", 256), want: strings.Repeat("a", 255)},
		{name: "truncates multibyte title by rune", title: strings.Repeat("好", 256), want: strings.Repeat("好", 255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, truncateTitle(tt.title))
		})
	}
}

func TestStreamProcessorPersistsFinalAgenticDataWithoutDeltas(t *testing.T) {
	messageID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	chatID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	index := int32(0)
	startPart := MessagePart{Type: PartTypeText, ID: "p0", Agent: "teacher"}
	completedPart := startPart
	completedPart.Text = "光合作用"

	provider := &fakeLLMProvider{chunks: []StreamChunk{
		agentChunk(AgentEvent{
			Type:       AgentEventCast,
			Characters: []Character{{ID: "teacher", DisplayName: "老師", Role: "teacher"}},
		}),
		agentChunk(AgentEvent{Type: AgentEventPartStart, Index: &index, Part: &startPart}),
		agentChunk(AgentEvent{Type: AgentEventDelta, Index: &index, Delta: "光合"}),
		agentChunk(AgentEvent{Type: AgentEventDelta, Index: &index, Delta: "作用"}),
		agentChunk(AgentEvent{Type: AgentEventPartEnd, Index: &index, Part: &completedPart}),
		agentChunk(AgentEvent{Type: AgentEventDone, FinishReason: "stop", Status: MessageStatusDone}),
	}}
	querier := &fakeChatQuerier{}
	hub := NewStreamHub()
	stream := hub.CreateStream(messageID)
	service := NewService(provider, querier, hub, zap.NewNop())

	service.streamProcessor(t.Context(), chatID, messageID, stream, AgentsRequest{}, false)

	require.Equal(t, MessageStatusDone, MessageStatus(querier.updateParams.Status))
	require.Equal(t, "光合作用", querier.updateParams.Content.String)
	require.NotContains(t, string(querier.updateParams.AgenticData), `"delta"`)

	var data AgenticData
	require.NoError(t, json.Unmarshal(querier.updateParams.AgenticData, &data))
	require.Equal(t, "stop", data.FinishReason)
	require.Equal(t, []MessagePart{completedPart}, data.Parts)
}

func TestCompletedAgenticStreamReconstructsSnapshotWithoutDeltas(t *testing.T) {
	data := AgenticData{
		Cast:         []Character{{ID: "teacher", DisplayName: "老師", Role: "teacher"}},
		Parts:        []MessagePart{{Type: PartTypeText, ID: "p0", Agent: "teacher", Text: "光合作用"}},
		FinishReason: "stop",
	}

	chunks := completedAgenticStream(data)
	var eventTypes []AgentEventType
	for chunk := range chunks {
		require.NotNil(t, chunk.Agentic)
		require.NotEqual(t, AgentEventDelta, chunk.Agentic.Type)
		eventTypes = append(eventTypes, chunk.Agentic.Type)
	}
	require.Equal(t, []AgentEventType{
		AgentEventCast,
		AgentEventAgentStart,
		AgentEventPartStart,
		AgentEventPartEnd,
		AgentEventAgentEnd,
		AgentEventDone,
	}, eventTypes)
}

type fakeChatQuerier struct {
	*Queries
	deleteParams       DeleteChatParams
	deleteRowsAffected int64
	deleteErr          error
	updateParams       UpdateMessageParams
}

func (q *fakeChatQuerier) UpdateMessage(_ context.Context, params UpdateMessageParams) (Message, error) {
	q.updateParams = params
	return Message{}, nil
}

type fakeLLMProvider struct {
	chunks []StreamChunk
	err    error
}

func (p *fakeLLMProvider) Stream(context.Context, AgentsRequest) (<-chan StreamChunk, <-chan error) {
	chunks := make(chan StreamChunk, len(p.chunks))
	for _, chunk := range p.chunks {
		chunks <- chunk
	}
	close(chunks)

	errs := make(chan error, 1)
	if p.err != nil {
		errs <- p.err
	}
	close(errs)
	return chunks, errs
}

func (p *fakeLLMProvider) GetTitle(context.Context, []ChatMessage) (string, error) {
	return "", nil
}

func (q *fakeChatQuerier) DeleteChat(_ context.Context, params DeleteChatParams) (int64, error) {
	q.deleteParams = params
	return q.deleteRowsAffected, q.deleteErr
}
