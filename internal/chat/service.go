package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

type ChatQuerier interface {
	CreateChat(ctx context.Context) (Chat, error)
	CreateMessage(ctx context.Context, arg CreateMessageParams) (Message, error)
	GetChat(ctx context.Context, id uuid.UUID) (Chat, error)
	GetMessage(ctx context.Context, id uuid.UUID) (Message, error)
	GetMessages(ctx context.Context, chatID uuid.UUID) ([]Message, error)
	UpdateMessage(ctx context.Context, arg UpdateMessageParams) (Message, error)
}

type ChatService struct {
	provider  LLMProvider
	querier   ChatQuerier
	streamHub *StreamHub
	logger    *zap.Logger
}

type MessageReturn struct {
	ID         uuid.UUID     `json:"id"`
	Content    string        `json:"content"`
	Role       MessageRole   `json:"role"`
	PreviousID uuid.UUID     `json:"previousID,omitempty"`
	Status     MessageStatus `json:"status"`
	CreatedAt  time.Time     `json:"createdAt"`
}

type CreateMessageReturn struct {
	Message        MessageReturn `json:"message"`
	ReplyMessageID uuid.UUID     `json:"replyMessageID"`
}

func NewService(provider LLMProvider, querier ChatQuerier, streamHub *StreamHub, logger *zap.Logger) *ChatService {
	return &ChatService{
		provider:  provider,
		querier:   querier,
		streamHub: streamHub,
		logger:    logger,
	}
}

func (s *ChatService) CreateChat(ctx context.Context) (uuid.UUID, error) {
	chat, err := s.querier.CreateChat(ctx)
	if err != nil {
		return uuid.New(), databaseutil.WrapDBError(err, s.logger, "create chat")
	}
	return chat.ID, nil
}

func (s *ChatService) fetchMessages(ctx context.Context, chatID uuid.UUID) ([]MessageReturn, error) {
	messages, err := s.querier.GetMessages(ctx, chatID)
	if err != nil {
		return nil, databaseutil.WrapDBErrorWithKeyValue(err, "messages", "chat_id", chatID.String(), s.logger, "get messages")
	}
	result := make([]MessageReturn, 0, len(messages))
	for _, msg := range messages {
		ret := MessageReturn{
			ID:        msg.ID,
			Content:   msg.Content.String,
			Role:      MessageRole(msg.Role),
			Status:    MessageStatus(msg.Status),
			CreatedAt: msg.CreatedAt.Time,
		}
		if msg.PreviousID.Valid {
			ret.PreviousID = uuid.UUID(msg.PreviousID.Bytes)
		}
		if msg.Content.String == "" {
			if stream, ok := s.streamHub.GetStream(msg.ID); ok {
				_, ret.Content, _ = stream.Get()
			}
		}
		result = append(result, ret)
	}
	return result, nil
}

func (s *ChatService) GetChat(ctx context.Context, chatID uuid.UUID) ([]MessageReturn, error) {
	chat, err := s.querier.GetChat(ctx, chatID)
	if err != nil {
		return nil, databaseutil.WrapDBErrorWithKeyValue(err, "chat", "chat_id", chatID.String(), s.logger, "get chat")
	}
	if chat.ID == uuid.Nil {
		return nil, handlerutil.NewNotFoundError("chat", "chat_id", chatID.String(), "")
	}
	result, err := s.fetchMessages(ctx, chatID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ChatService) CreateMessage(ctx context.Context, chatID uuid.UUID, content string, previousID uuid.UUID) (CreateMessageReturn, error) {

	chat, err := s.querier.GetChat(ctx, chatID)
	if err != nil {
		return CreateMessageReturn{}, databaseutil.WrapDBErrorWithKeyValue(err, "chat", "chat_id", chatID.String(), s.logger, "get chat")
	}
	if chat.ID == uuid.Nil {
		return CreateMessageReturn{}, handlerutil.NewNotFoundError("chat", "chat_id", chatID.String(), "")
	}

	// Create message in DB
	userMessage, err := s.querier.CreateMessage(ctx, CreateMessageParams{
		ChatID: chatID,
		Content: pgtype.Text{
			String: content,
			Valid:  true,
		},
		Role:   string(MessageRoleUser),
		Status: string(MessageStatusDone),
		PreviousID: pgtype.UUID{
			Bytes: [16]byte(previousID),
			Valid: previousID != uuid.Nil,
		},
	})
	if err != nil {
		return CreateMessageReturn{}, databaseutil.WrapDBError(err, s.logger, "create message")
	}

	allMessages, err := s.fetchMessages(ctx, chatID)
	if err != nil {
		return CreateMessageReturn{}, err
	}
	history := createChatHistory(allMessages)

	// create response message in DB
	llmMessage, err := s.querier.CreateMessage(ctx, CreateMessageParams{
		ChatID: chatID,
		Content: pgtype.Text{
			String: "",
			Valid:  true,
		},
		Role:   string(MessageRoleAssistant),
		Status: string(MessageStatusStreaming),
		PreviousID: pgtype.UUID{
			Bytes: [16]byte(userMessage.ID),
			Valid: true,
		},
	})
	if err != nil {
		return CreateMessageReturn{}, databaseutil.WrapDBError(err, s.logger, "create response message")
	}

	// create provider request
	providerReq := CreateChatCompletionRequest{
		Messages: history,
		Stream:   true,
	}

	streamEvent := s.streamHub.CreateStream(llmMessage.ID)
	go s.streamProcessor(context.Background(), llmMessage.ID, streamEvent, providerReq)

	return CreateMessageReturn{
		Message: MessageReturn{
			ID:         userMessage.ID,
			Content:    userMessage.Content.String,
			Role:       MessageRole(userMessage.Role),
			Status:     MessageStatus(userMessage.Status),
			CreatedAt:  userMessage.CreatedAt.Time,
			PreviousID: previousID,
		},
		ReplyMessageID: llmMessage.ID,
	}, nil

}

func (s *ChatService) Stream(ctx context.Context, messageID uuid.UUID) (bool, <-chan StreamDelta, <-chan error, func()) {
	streamEvent, ok := s.streamHub.GetStream(messageID)
	if !ok {
		return false, nil, nil, func() {}
	}
	llmCh, errCh, cancel := streamEvent.Subscribe()
	return ok, llmCh, errCh, cancel
}

func (s *ChatService) streamProcessor(ctx context.Context, messageID uuid.UUID, streamEvent *StreamEvent, providerReq CreateChatCompletionRequest) {
	llmCh, errCh := s.provider.Stream(ctx, providerReq)
	endFlag := false
	for !endFlag && (llmCh != nil || errCh != nil) {
		select {
		case <-ctx.Done():
			streamEvent.Fail(ctx.Err())
			endFlag = true
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamEvent.Fail(err)
				endFlag = true
			}
		case chunk, ok := <-llmCh:
			if !ok {
				llmCh = nil
				continue
			} else {
				if chunk.IsFinished {
					streamEvent.Complete()
					endFlag = true
				} else {
					streamEvent.AppendDelta(chunk)
				}
			}
		}
	}

	status, fullChunk, err := streamEvent.Get()
	if err != nil {
		SSEError(err, s.logger)
	}
	s.streamHub.DeleteStream(messageID)
	// Use a fresh context so client disconnect doesn't prevent persisting the final state.
	updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = s.querier.UpdateMessage(updateCtx, UpdateMessageParams{
		ID: messageID,
		Content: pgtype.Text{
			String: fullChunk,
			Valid:  true,
		},
		Status: string(status),
	})
	if err != nil {
		SSEError(err, s.logger)
	}

}

func (s *ChatService) ValidatePreviousID(ctx context.Context, previousID uuid.UUID, chatID uuid.UUID) error {
	if previousID == uuid.Nil {
		return nil
	}
	msg, err := s.querier.GetMessage(ctx, previousID)
	if err != nil {
		return databaseutil.WrapDBErrorWithKeyValue(err, "message", "ID", previousID.String(), s.logger, "validate previous id")
	}
	if msg.ID == uuid.Nil {
		return handlerutil.NewNotFoundError("message", "previous_id", previousID.String(), "")
	}
	if msg.ChatID != chatID {
		return handlerutil.NewNotFoundError("message", "previous_id", previousID.String(), fmt.Sprintf("previous message does not belong to the same chat: previous_id=%s, chat_id=%s", previousID.String(), chatID.String()))
	}
	return nil
}

func createChatHistory(allMessages []MessageReturn) []ChatMessage {
	if len(allMessages) == 0 {
		return nil
	}

	allMessagesMap := make(map[uuid.UUID]MessageReturn, len(allMessages))
	for _, msg := range allMessages {
		allMessagesMap[msg.ID] = msg
	}

	history := make([]ChatMessage, 0, len(allMessages))
	idNow := allMessages[len(allMessages)-1].ID

	for {
		msg, ok := allMessagesMap[idNow]
		if !ok {
			break
		}
		history = append(history, ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
		if msg.PreviousID == uuid.Nil {
			break
		}
		idNow = msg.PreviousID
	}

	result := make([]ChatMessage, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		result = append(result, history[i])
	}

	return result
}

func SSEError(err error, logger *zap.Logger) {
	logger.Warn("Handling SSE Error", zap.String("problem", "SSE Error"), zap.Error(err))
}

// temp
var ErrStatus502 = errors.New("message has error status")
