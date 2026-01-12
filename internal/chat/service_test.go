package chat_test

import (
	"context"
	"sciedu-backend/internal/chat"
	"sciedu-backend/internal/chat/mocks"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestChatService_ForwardsChunksAndCompletes(t *testing.T) {
	provider := new(mocks.LLMProvider)

	inChunks := make(chan chat.ChatCompletionChunk)
	inErrs := make(chan error, 1)

	// Arrange mock: provider.StreamChat returns our controlled channels
	provider.
		On("StreamChat", mock.Anything, mock.AnythingOfType("chat.CreateChatCompletionRequest")).
		Return((<-chan chat.ChatCompletionChunk)(inChunks), (<-chan error)(inErrs))

	svc := chat.NewChatService(provider)

	ctx := context.Background()
	req := chat.CreateChatCompletionRequest{
		Messages: []chat.ChatMessage{{Role: chat.ChatRole{User: "u", Assistant: "a", System: "s"}, Content: "hi"}},
		Stream:   false, // service should force true
	}

	outChunks, outErrs := svc.StreamChat(ctx, req)

	// Drive upstream
	go func() {
		inChunks <- chat.ChatCompletionChunk{Delta: "A", IsFinished: false}
		inChunks <- chat.ChatCompletionChunk{Delta: "B", IsFinished: false}
		close(inChunks)
		close(inErrs)
	}()

	// Assert chunks forwarded
	got1 := <-outChunks
	require.Equal(t, "A", got1.Delta)

	got2 := <-outChunks
	require.Equal(t, "B", got2.Delta)

	// After upstream closes, downstream should close
	_, ok := <-outChunks
	require.False(t, ok)

	// errs should close without error
	select {
	case err, ok := <-outErrs:
		if ok {
			require.NoError(t, err)
		}
	case <-time.After(200 * time.Millisecond):
		// ok to not receive; channel may simply close
	}
}

func TestChatService_PropagatesContextCancellation(t *testing.T) {
	provider := new(mocks.LLMProvider)

	inChunks := make(chan chat.ChatCompletionChunk)
	inErrs := make(chan error, 1)

	var capturedCtx context.Context

	provider.
		On("StreamChat", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			capturedCtx = args.Get(0).(context.Context)
		}).
		Return((<-chan chat.ChatCompletionChunk)(inChunks), (<-chan error)(inErrs))

	svc := chat.NewChatService(provider)

	ctx, cancel := context.WithCancel(context.Background())
	req := chat.CreateChatCompletionRequest{
		Messages: []chat.ChatMessage{{Role: chat.ChatRole{User: "u", Assistant: "a", System: "s"}, Content: "hi"}},
		Stream:   true,
	}

	outChunks, _ := svc.StreamChat(ctx, req)

	// Ensure provider got the ctx
	require.NotNil(t, capturedCtx)

	// Cancel downstream
	cancel()

	// Service should stop promptly (outChunks should close)
	select {
	case _, ok := <-outChunks:
		require.False(t, ok)
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected outChunks to close after ctx cancellation")
	}

	// Also ensure the captured ctx is cancelled
	select {
	case <-capturedCtx.Done():
		// ok
	case <-time.After(300 * time.Millisecond):
		t.Fatal("expected provider ctx to be cancelled")
	}
}
