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
	CreatedAt  time.Time     `json:"created_at"`
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

func (s *ChatService) GetChat(ctx context.Context, chatID uuid.UUID) ([]MessageReturn, error) {
	chat, err := s.querier.GetChat(ctx, chatID)
	if err != nil {
		return nil, databaseutil.WrapDBError(err, s.logger, "get chat")
	}
	//not exist
	if chat.ID == uuid.Nil {
		err = fmt.Errorf("chat not found: chat_id=%s", chatID.String())
		errors.As(err, &handlerutil.NotFoundError{})
		return nil, err
	}
	messages, err := s.querier.GetMessages(ctx, chatID)
	if err != nil {
		return nil, databaseutil.WrapDBErrorWithKeyValue(err, "messages", "chat_id", chatID.String(), s.logger, "get messages")
	}
	// Convert to return type
	var result []MessageReturn
	err = nil
	for _, msg := range messages {
		ret := MessageReturn{
			ID:        msg.ID,
			Content:   msg.Content.String,
			Role:      MessageRole(msg.Role),
			Status:    MessageStatus(msg.Status),
			CreatedAt: msg.CreatedAt.Time,
		}
		if msg.PreviousID.Valid {
			ret.PreviousID = msg.PreviousID.Bytes
		}
		result = append(result, ret)
		if MessageStatus(msg.Status) == MessageStatusError {
			err = fmt.Errorf("MessageID: %s, has error status", msg.ID.String())
			// hei, This is supposed to be an 502 error, but Summer's HttpWriter does not support 502 now, so I have to use 500 for now. wtf
			continue
		}
		if msg.Content.String == "" {
			stream, ok := s.streamHub.GetStream(msg.ID)
			if ok {
				_, ret.Content, _ = stream.Get()
			}
		}
	}

	return result, err
}

func (s *ChatService) CreateMessage(ctx context.Context, chatID uuid.UUID, content string, previousID uuid.UUID) (CreateMessageReturn, error) {

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
			Bytes: previousID,
			Valid: previousID != uuid.Nil,
		},
	})
	if err != nil {
		return CreateMessageReturn{}, databaseutil.WrapDBError(err, s.logger, "create message")
	}

	allMessages, err := s.GetChat(ctx, chatID)
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
			Bytes: userMessage.ID,
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

	go s.streamProcessor(ctx, llmMessage.ID, providerReq)

	return CreateMessageReturn{
		Message: MessageReturn{
			ID:        userMessage.ID,
			Content:   userMessage.Content.String,
			Role:      MessageRole(userMessage.Role),
			Status:    MessageStatus(userMessage.Status),
			CreatedAt: userMessage.CreatedAt.Time,
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

func (s *ChatService) streamProcessor(ctx context.Context, messageID uuid.UUID, providerReq CreateChatCompletionRequest) {
	streamEvent := s.streamHub.CreateStream(messageID)
	llmCh, errCh := s.provider.Stream(ctx, providerReq)
	endFlag := false
	for endFlag != true && (llmCh != nil || errCh != nil) {
		select {
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamEvent.Fail(err)
				endFlag = true
				break
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
	_, err = s.querier.UpdateMessage(ctx, UpdateMessageParams{
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
		err := fmt.Errorf("previous message not found: previous_id=%s", previousID.String())
		errors.As(err, &handlerutil.NotFoundError{})
		return err
	}
	if msg.ChatID != chatID {
		err = fmt.Errorf("previous message does not belong to the same chat: previous_id=%s, chat_id=%s", previousID.String(), chatID.String())
		errors.As(err, &handlerutil.ErrNotFound)
		return err
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

	for msg, ok := allMessagesMap[idNow]; ok && msg.PreviousID != idNow; {
		history = append(history, ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
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
