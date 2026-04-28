package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	ErrInvalidChat    = errors.New("invalid chat")
	ErrStreamNotFound = errors.New("stream not found")
)

type Store interface {
	CreateChat(ctx context.Context) (uuid.UUID, error)
	ListMessages(ctx context.Context, chatID uuid.UUID) ([]Message, error)
	CreateUserMessageWithReply(ctx context.Context, chatID uuid.UUID, req SendMessageRequest) (Message, uuid.UUID, error)
	ListMessagesForReply(ctx context.Context, replyID uuid.UUID) ([]Message, error)
	GetMessage(ctx context.Context, id uuid.UUID) (Message, error)
	UpdateMessage(ctx context.Context, id uuid.UUID, content string, status Status) error
}

type Streamer interface {
	Stream(ctx context.Context, messages []Message) (<-chan string, <-chan error)
}

type Service struct {
	store    Store
	streamer Streamer
	streams  *streamRegistry
	logger   *zap.Logger
}

func NewService(store Store, streamer Streamer, logger *zap.Logger) *Service {
	return &Service{
		store:    store,
		streamer: streamer,
		streams:  newStreamRegistry(),
		logger:   logger,
	}
}

func (s *Service) CreateChat(ctx context.Context) (uuid.UUID, error) {
	return s.store.CreateChat(ctx)
}

func (s *Service) ListMessages(ctx context.Context, chatID uuid.UUID) ([]Message, error) {
	return s.store.ListMessages(ctx, chatID)
}

func (s *Service) SendMessage(ctx context.Context, chatID uuid.UUID, req SendMessageRequest) (Message, uuid.UUID, error) {
	if strings.TrimSpace(req.Content) == "" {
		return Message{}, uuid.Nil, fmt.Errorf("%w: content is required", ErrInvalidChat)
	}
	message, replyID, err := s.store.CreateUserMessageWithReply(ctx, chatID, req)
	if err != nil {
		return Message{}, uuid.Nil, err
	}

	stream := s.streams.create(replyID)
	s.logger.Info("created chat reply placeholder",
		zap.String("chat_id", chatID.String()),
		zap.String("user_message_id", message.ID.String()),
		zap.String("reply_message_id", replyID.String()),
	)
	go s.generateReply(context.Background(), replyID, stream)

	return message, replyID, nil
}

func (s *Service) SubscribeStream(ctx context.Context, messageID uuid.UUID) (string, <-chan StreamEvent, func(), error) {
	message, err := s.store.GetMessage(ctx, messageID)
	if err != nil {
		return "", nil, nil, err
	}
	if message.Role != RoleAssistant {
		return "", nil, nil, fmt.Errorf("%w: message is not an assistant reply", ErrInvalidChat)
	}

	stream, ok := s.streams.get(messageID)
	if !ok {
		return "", nil, nil, ErrStreamNotFound
	}

	initialContent, events, unsubscribe := stream.subscribe()
	return initialContent, events, unsubscribe, nil
}

func (s *Service) generateReply(ctx context.Context, replyID uuid.UUID, stream *activeStream) {
	defer s.streams.remove(replyID)
	s.logger.Info("starting chat reply generation", zap.String("reply_message_id", replyID.String()))

	if err := s.store.UpdateMessage(ctx, replyID, "", StatusStreaming); err != nil {
		s.logger.Error("failed to mark reply as streaming",
			zap.String("reply_message_id", replyID.String()),
			zap.Error(err),
		)
		stream.fail(err)
		return
	}

	messages, err := s.store.ListMessagesForReply(ctx, replyID)
	if err != nil {
		_ = s.store.UpdateMessage(ctx, replyID, stream.currentContent(), StatusFailed)
		s.logger.Error("failed to load chat branch for reply generation",
			zap.String("reply_message_id", replyID.String()),
			zap.Error(err),
		)
		stream.fail(err)
		return
	}
	s.logger.Info("loaded chat branch for reply generation",
		zap.String("reply_message_id", replyID.String()),
		zap.Int("message_count", len(messages)),
	)

	chunks, streamErrs := s.streamer.Stream(ctx, messages)
	for chunk := range chunks {
		content := stream.append(chunk)
		if err := s.store.UpdateMessage(ctx, replyID, content, StatusStreaming); err != nil {
			_ = s.store.UpdateMessage(ctx, replyID, content, StatusFailed)
			s.logger.Error("failed to persist streaming reply content",
				zap.String("reply_message_id", replyID.String()),
				zap.Int("content_length", len(content)),
				zap.Error(err),
			)
			stream.fail(err)
			return
		}
	}

	if err := <-streamErrs; err != nil {
		_ = s.store.UpdateMessage(ctx, replyID, stream.currentContent(), StatusFailed)
		s.logger.Error("llm stream failed",
			zap.String("reply_message_id", replyID.String()),
			zap.Int("content_length", len(stream.currentContent())),
			zap.Error(err),
		)
		stream.fail(err)
		return
	}

	if err := s.store.UpdateMessage(ctx, replyID, stream.currentContent(), StatusCompleted); err != nil {
		s.logger.Error("failed to mark reply as completed",
			zap.String("reply_message_id", replyID.String()),
			zap.Int("content_length", len(stream.currentContent())),
			zap.Error(err),
		)
		stream.fail(err)
		return
	}
	s.logger.Info("completed chat reply generation",
		zap.String("reply_message_id", replyID.String()),
		zap.Int("content_length", len(stream.currentContent())),
	)
	stream.done()
}

type streamRegistry struct {
	mu      sync.Mutex
	streams map[uuid.UUID]*activeStream
}

func newStreamRegistry() *streamRegistry {
	return &streamRegistry{streams: make(map[uuid.UUID]*activeStream)}
}

func (r *streamRegistry) create(messageID uuid.UUID) *activeStream {
	r.mu.Lock()
	defer r.mu.Unlock()

	stream := newActiveStream()
	r.streams[messageID] = stream
	return stream
}

func (r *streamRegistry) get(messageID uuid.UUID) (*activeStream, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stream, ok := r.streams[messageID]
	return stream, ok
}

func (r *streamRegistry) remove(messageID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.streams, messageID)
}

type activeStream struct {
	mu          sync.Mutex
	content     strings.Builder
	subscribers map[chan StreamEvent]struct{}
}

func newActiveStream() *activeStream {
	return &activeStream{subscribers: make(map[chan StreamEvent]struct{})}
}

func (s *activeStream) subscribe() (string, <-chan StreamEvent, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events := make(chan StreamEvent, 32)
	s.subscribers[events] = struct{}{}
	unsubscribe := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subscribers[events]; !ok {
			return
		}
		delete(s.subscribers, events)
		close(events)
	}

	return s.content.String(), events, unsubscribe
}

func (s *activeStream) append(chunk string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.content.WriteString(chunk)
	s.broadcastLocked(StreamEvent{Type: StreamEventDelta, Content: chunk})
	return s.content.String()
}

func (s *activeStream) currentContent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.content.String()
}

func (s *activeStream) done() {
	s.closeWith(StreamEvent{Type: StreamEventDone})
}

func (s *activeStream) fail(err error) {
	s.closeWith(StreamEvent{Type: StreamEventError, Err: err})
}

func (s *activeStream) closeWith(event StreamEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.broadcastLocked(event)
	for subscriber := range s.subscribers {
		close(subscriber)
		delete(s.subscribers, subscriber)
	}
}

func (s *activeStream) broadcastLocked(event StreamEvent) {
	for subscriber := range s.subscribers {
		subscriber <- event
	}
}
