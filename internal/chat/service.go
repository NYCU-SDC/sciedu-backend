package chat

import (
	"context"
	"fmt"
	"time"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type ChatQuerier interface {
	CreateChat(ctx context.Context) (Chat, error)
	CreateMessage(ctx context.Context, arg CreateMessageParams) (Message, error)
	GetChat(ctx context.Context, id uuid.UUID) (Chat, error)
	GetMessage(ctx context.Context, id uuid.UUID) (Message, error)
	GetMessages(ctx context.Context, chatID uuid.UUID) ([]Message, error)
}

type ChatService struct {
	provider LLMProvider
	querier  ChatQuerier
	logger   *zap.Logger
}

type MessageReturn struct {
	ID         uuid.UUID  `json:"id"`
	Content    string     `json:"content"`
	Role       ChatRole   `json:"role"`
	PreviousID uuid.UUID  `json:"previous_id,omitempty"`
	Status     ChatStatus `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
}

func NewService(provider LLMProvider, querier ChatQuerier, logger *zap.Logger) *ChatService {
	return &ChatService{
		provider: provider,
		querier:  querier,
		logger:   logger,
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
		return nil, fmt.Errorf("chat not found: chat_id=%s", chatID.String())
	}
	messages, err := s.querier.GetMessages(ctx, chatID)
	if err != nil {
		return nil, databaseutil.WrapDBErrorWithKeyValue(err, "messages", "chat_id", chatID.String(), s.logger, "get messages")
	}
	// Convert to return type
	var result []MessageReturn
	for _, msg := range messages {
		ret := MessageReturn{
			ID:        msg.ID,
			Content:   msg.Content.String,
			Role:      ChatRole(msg.Role.(string)),
			Status:    ChatStatus(msg.Status),
			CreatedAt: msg.CreatedAt.Time,
		}
		if msg.PreviousID.Valid {
			ret.PreviousID = msg.PreviousID.Bytes
		}
		result = append(result, ret)
	}
	return result, nil
}
