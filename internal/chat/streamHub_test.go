package chat

import (
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

	select {
	case gotErr := <-errs:
		require.ErrorIs(t, gotErr, wantErr)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream error")
	}
}

func TestStreamEventAppendDeltaPublishesRuneDeltas(t *testing.T) {
	stream := NewStreamHub().CreateStream(uuid.MustParse("00000000-0000-0000-0000-000000000001"))
	chunks, _, cancel := stream.Subscribe()
	defer cancel()

	stream.AppendDelta(StreamDelta{Delta: "Hi好"})

	require.Equal(t, StreamDelta{Delta: "H"}, receiveChunk(t, chunks))
	require.Equal(t, StreamDelta{Delta: "i"}, receiveChunk(t, chunks))
	require.Equal(t, StreamDelta{Delta: "好"}, receiveChunk(t, chunks))

	_, fullContent, err := stream.Get()
	require.NoError(t, err)
	require.Equal(t, "Hi好", fullContent)
}

func TestCompletedStreamPublishesRuneDeltas(t *testing.T) {
	chunks := completedStream("OK了")

	require.Equal(t, StreamDelta{Delta: "O"}, receiveChunk(t, chunks))
	require.Equal(t, StreamDelta{Delta: "K"}, receiveChunk(t, chunks))
	require.Equal(t, StreamDelta{Delta: "了"}, receiveChunk(t, chunks))
	require.Equal(t, StreamDelta{Delta: "", IsFinished: true}, receiveChunk(t, chunks))
}
