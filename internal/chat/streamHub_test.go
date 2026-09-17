package chat

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestStreamEventFailPublishesSubscriberError(t *testing.T) {
	stream := NewStreamHub().CreateStream(uuid.MustParse("00000000-0000-0000-0000-000000000001"))
	_, errs, cancel := stream.Subscribe()
	defer cancel()

	wantErr := errors.New("upstream failed")
	stream.Fail(wantErr)
	require.ErrorIs(t, receiveErr(t, errs), wantErr)
}

func TestStreamEventAggregatesAgenticPartsWithoutPersistingDeltas(t *testing.T) {
	stream := NewStreamHub().CreateStream(uuid.MustParse("00000000-0000-0000-0000-000000000001"))
	index := int32(0)
	part := MessagePart{Type: PartTypeText, ID: "p0", Agent: "teacher"}

	require.NoError(t, stream.Apply(agentChunk(AgentEvent{
		Type:       AgentEventCast,
		Characters: []Character{{ID: "teacher", DisplayName: "老師", Role: "teacher"}},
	})))
	require.NoError(t, stream.Apply(agentChunk(AgentEvent{Type: AgentEventPartStart, Index: &index, Part: &part})))
	require.NoError(t, stream.Apply(agentChunk(AgentEvent{Type: AgentEventDelta, Index: &index, Delta: "光合"})))
	require.NoError(t, stream.Apply(agentChunk(AgentEvent{Type: AgentEventDelta, Index: &index, Delta: "作用"})))

	status, content, data, agentic, err := stream.Get()
	require.NoError(t, err)
	require.True(t, agentic)
	require.Equal(t, MessageStatusStreaming, status)
	require.Equal(t, "光合作用", content)
	require.Equal(t, "光合作用", data.Parts[0].Text)

	encoded, err := json.Marshal(data)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"delta"`)

	completedPart := part
	completedPart.Text = "光合作用"
	require.NoError(t, stream.Apply(agentChunk(AgentEvent{Type: AgentEventPartEnd, Index: &index, Part: &completedPart})))
	require.NoError(t, stream.Apply(agentChunk(AgentEvent{
		Type:         AgentEventDone,
		FinishReason: "stop",
		Status:       MessageStatusDone,
	})))

	status, content, data, _, err = stream.Get()
	require.NoError(t, err)
	require.Equal(t, MessageStatusDone, status)
	require.Equal(t, "光合作用", content)
	require.Equal(t, "stop", data.FinishReason)
}

func TestStreamEventSubscribeReplaysBufferedEvents(t *testing.T) {
	stream := NewStreamHub().CreateStream(uuid.MustParse("00000000-0000-0000-0000-000000000001"))
	index := int32(0)
	require.NoError(t, stream.Apply(agentChunk(AgentEvent{Type: AgentEventDelta, Index: &index, Delta: "before"})))

	chunks, _, cancel := stream.Subscribe()
	defer cancel()
	require.Equal(t, "before", receiveStreamChunk(t, chunks).Agentic.Delta)

	require.NoError(t, stream.Apply(agentChunk(AgentEvent{Type: AgentEventDelta, Index: &index, Delta: "after"})))
	require.Equal(t, "after", receiveStreamChunk(t, chunks).Agentic.Delta)
}

func TestCompletedStreamPublishesRuneDeltas(t *testing.T) {
	chunks := completedStream("OK了")

	require.Equal(t, "O", receiveStreamChunk(t, chunks).Legacy.Delta)
	require.Equal(t, "K", receiveStreamChunk(t, chunks).Legacy.Delta)
	require.Equal(t, "了", receiveStreamChunk(t, chunks).Legacy.Delta)
	require.True(t, receiveStreamChunk(t, chunks).Terminal())
}

func TestStreamEventApplyDoesNotBlockOnSlowSubscriber(t *testing.T) {
	stream := NewStreamHub().CreateStream(uuid.MustParse("00000000-0000-0000-0000-000000000001"))
	chunks, errs, cancel := stream.Subscribe()
	defer cancel()

	for i := 0; i < cap(chunks); i++ {
		require.NoError(t, stream.Apply(StreamChunk{Legacy: &StreamDelta{Delta: "x"}}))
	}

	published := make(chan struct{})
	go func() {
		_ = stream.Apply(StreamChunk{Legacy: &StreamDelta{Delta: "overflow"}})
		close(published)
	}()

	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on slow subscriber")
	}
	require.ErrorIs(t, receiveErr(t, errs), errSlowStreamSubscriber)
}
