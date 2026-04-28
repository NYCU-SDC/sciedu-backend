package chat

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeChatStore struct {
	mu       sync.Mutex
	messages []Message
	updates  []Message
	replyID  uuid.UUID
}

func (f *fakeChatStore) CreateChat(ctx context.Context) (uuid.UUID, error) { return uuid.New(), nil }
func (f *fakeChatStore) ListMessages(ctx context.Context, chatID uuid.UUID) ([]Message, error) {
	return f.messages, nil
}
func (f *fakeChatStore) CreateUserMessageWithReply(ctx context.Context, chatID uuid.UUID, req SendMessageRequest) (Message, uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.replyID == uuid.Nil {
		f.replyID = uuid.New()
	}
	return Message{ID: uuid.New(), Content: req.Content, Role: RoleUser, Status: StatusCreated, ChatID: chatID}, f.replyID, nil
}
func (f *fakeChatStore) ListMessagesForReply(ctx context.Context, replyID uuid.UUID) ([]Message, error) {
	return f.messages, nil
}
func (f *fakeChatStore) GetMessage(ctx context.Context, id uuid.UUID) (Message, error) {
	return Message{ID: id, Role: RoleAssistant, Status: StatusStreaming, ChatID: uuid.New()}, nil
}
func (f *fakeChatStore) UpdateMessage(ctx context.Context, id uuid.UUID, content string, status Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.updates = append(f.updates, Message{ID: id, Content: content, Status: status})
	return nil
}

func (f *fakeChatStore) lastUpdate() Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.updates[len(f.updates)-1]
}

type fakeStreamer struct {
	chunks []string
	err    error
}

func (f fakeStreamer) Stream(ctx context.Context, messages []Message) (<-chan string, <-chan error) {
	chunks := make(chan string)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		defer close(errs)
		for _, chunk := range f.chunks {
			chunks <- chunk
		}
		if f.err != nil {
			errs <- f.err
		}
	}()
	return chunks, errs
}

type controlledStreamer struct {
	chunks chan string
	errs   chan error
}

func newControlledStreamer() *controlledStreamer {
	return &controlledStreamer{
		chunks: make(chan string),
		errs:   make(chan error),
	}
}

func (s *controlledStreamer) Stream(ctx context.Context, messages []Message) (<-chan string, <-chan error) {
	return s.chunks, s.errs
}

func TestServiceSendMessageValidation(t *testing.T) {
	service := NewService(&fakeChatStore{}, fakeStreamer{})

	_, _, err := service.SendMessage(context.Background(), uuid.New(), SendMessageRequest{})
	require.Error(t, err)

	_, _, err = service.SendMessage(context.Background(), uuid.New(), SendMessageRequest{Content: "hello"})
	require.NoError(t, err)
}

func TestServiceStreamReplyStoresCompletedContent(t *testing.T) {
	store := &fakeChatStore{
		messages: []Message{{ID: uuid.New(), Role: RoleUser, Content: "hello", Status: StatusCreated}},
	}
	streamer := newControlledStreamer()
	service := NewService(store, streamer)

	_, replyID, err := service.SendMessage(context.Background(), uuid.New(), SendMessageRequest{Content: "hello"})
	require.NoError(t, err)

	initial, events, unsubscribe, err := service.SubscribeStream(context.Background(), replyID)
	require.NoError(t, err)
	defer unsubscribe()
	require.Empty(t, initial)

	streamer.chunks <- "hi"
	event := <-events
	require.Equal(t, StreamEventDelta, event.Type)
	require.Equal(t, "hi", event.Content)
	require.Equal(t, StatusStreaming, store.lastUpdate().Status)
	require.Equal(t, "hi", store.lastUpdate().Content)

	close(streamer.chunks)
	close(streamer.errs)

	event = <-events
	require.Equal(t, StreamEventDone, event.Type)
	_, ok := <-events
	require.False(t, ok)
	require.Equal(t, StatusCompleted, store.lastUpdate().Status)
	require.Equal(t, "hi", store.lastUpdate().Content)
}
