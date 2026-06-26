package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	databaseutil "github.com/NYCU-SDC/summer/pkg/database"
	handlerutil "github.com/NYCU-SDC/summer/pkg/handler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/zap"
)

type ChatQuerier interface {
	CreateChat(ctx context.Context, arg CreateChatParams) (Chat, error)
	CreateMessage(ctx context.Context, arg CreateMessageParams) (Message, error)
	GetChat(ctx context.Context, id uuid.UUID) (Chat, error)
	GetChatByUser(ctx context.Context, arg GetChatByUserParams) (Chat, error)
	GetMessage(ctx context.Context, id uuid.UUID) (Message, error)
	GetMessageByUser(ctx context.Context, arg GetMessageByUserParams) (Message, error)
	GetMessages(ctx context.Context, chatID uuid.UUID) ([]Message, error)
	UpdateMessage(ctx context.Context, arg UpdateMessageParams) (Message, error)
	UpdateChat(ctx context.Context, arg UpdateChatParams) (Chat, error)
	ListChatsByUser(ctx context.Context, arg ListChatsByUserParams) ([]Chat, error)
	CountChatsByUser(ctx context.Context, userID uuid.UUID) (int64, error)
	DeleteChat(ctx context.Context, id uuid.UUID) error
}

type ChatService struct {
	provider  LLMProvider
	querier   ChatQuerier
	streamHub *StreamHub
	logger    *zap.Logger
}

func normalizePagination(page, pageSize int32) (int32, int32) {
	if page < 1 {
		page = defaultPage
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

type ChatReturn struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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

func (s *ChatService) CreateChat(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	chat, err := s.querier.CreateChat(ctx, CreateChatParams{
		UserID: userID,
		Title:  "",
	})
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

func (s *ChatService) GetChat(ctx context.Context, userID uuid.UUID, chatID uuid.UUID) (Chat, []MessageReturn, error) {
	chat, err := s.querier.GetChatByUser(ctx, GetChatByUserParams{
		ID:     chatID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Chat{}, nil, handlerutil.NewNotFoundError("chat", "chat_id", chatID.String(), "")
		}
		return Chat{}, nil, databaseutil.WrapDBErrorWithKeyValue(err, "chat", "chat_id", chatID.String(), s.logger, "get chat")
	}
	if chat.ID == uuid.Nil {
		return Chat{}, nil, handlerutil.NewNotFoundError("chat", "chat_id", chatID.String(), "")
	}
	result, err := s.fetchMessages(ctx, chatID)
	if err != nil {
		return Chat{}, nil, err
	}
	return chat, result, nil
}

func (s *ChatService) ListChats(ctx context.Context, userID uuid.UUID, page, pageSize int32) (ChatPage, error) {
	page, pageSize = normalizePagination(page, pageSize)

	total, err := s.querier.CountChatsByUser(ctx, userID)
	if err != nil {
		return ChatPage{}, databaseutil.WrapDBError(err, s.logger, "count chats by user")
	}

	offset := (page - 1) * pageSize
	chats, err := s.querier.ListChatsByUser(ctx, ListChatsByUserParams{
		UserID: userID,
		Limit:  pageSize,
		Offset: offset,
	})
	if err != nil {
		return ChatPage{}, databaseutil.WrapDBError(err, s.logger, "list chats by user")
	}

	totalItems := int32(total)
	totalPages := int32(0)
	if totalItems > 0 {
		totalPages = (totalItems + pageSize - 1) / pageSize
	}

	items := make([]ChatReturn, 0, len(chats))
	for _, c := range chats {
		items = append(items, ChatReturn{
			ID:        c.ID,
			Title:     c.Title,
			CreatedAt: c.CreatedAt.Time,
			UpdatedAt: c.UpdatedAt.Time,
		})
	}

	return ChatPage{
		Items:       items,
		TotalPages:  totalPages,
		TotalItems:  totalItems,
		CurrentPage: page,
		PageSize:    pageSize,
		HasNextPage: page < totalPages,
	}, nil
}

func (s *ChatService) DeleteChat(ctx context.Context, chatID uuid.UUID, userID uuid.UUID) error {
	chat, err := s.querier.GetChat(ctx, chatID)
	if err != nil {
		return databaseutil.WrapDBErrorWithKeyValue(err, "chat", "chat_id", chatID.String(), s.logger, "get chat for delete")
	}
	if chat.ID == uuid.Nil {
		return handlerutil.NewNotFoundError("chat", "chat_id", chatID.String(), "")
	}
	if chat.UserID != userID {
		return handlerutil.NewNotFoundError("chat", "chat_id", chatID.String(), "chat does not belong to the user")
	}

	err = s.querier.DeleteChat(ctx, chatID)
	if err != nil {
		return databaseutil.WrapDBErrorWithKeyValue(err, "chat", "chat_id", chatID.String(), s.logger, "delete chat")
	}
	return nil
}

func (s *ChatService) CreateMessage(ctx context.Context, userID uuid.UUID, chatID uuid.UUID, content string, previousID uuid.UUID) (CreateMessageReturn, error) {

	chat, err := s.querier.GetChatByUser(ctx, GetChatByUserParams{
		ID:     chatID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CreateMessageReturn{}, handlerutil.NewNotFoundError("chat", "chat_id", chatID.String(), "")
		}
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

	// update chat
	createTitle := false
	newTitle := chat.Title
	if newTitle == "" {
		newTitle = content
		createTitle = true
	}
	if _, err = s.querier.UpdateChat(ctx, UpdateChatParams{
		ID:    chatID,
		Title: newTitle,
	}); err != nil {
		return CreateMessageReturn{}, databaseutil.WrapDBError(err, s.logger, "update chat title")
	}

	// create provider request
	providerReq := CreateChatCompletionRequest{
		Messages: history,
		Stream:   true,
	}

	streamEvent := s.streamHub.CreateStream(llmMessage.ID)
	go s.streamProcessor(context.Background(), chatID, llmMessage.ID, streamEvent, providerReq, createTitle)

	return CreateMessageReturn{
		Message: MessageReturn{
			ID:         userMessage.ID,
			Content:    userMessage.Content.String,
			Role:       MessageRole(userMessage.Role),
			Status:     MessageStatus(userMessage.Status),
			CreatedAt:  userMessage.CreatedAt.Time,
			PreviousID: previousID, //!
		},
		ReplyMessageID: llmMessage.ID,
	}, nil

}

func (s *ChatService) Stream(ctx context.Context, userID uuid.UUID, messageID uuid.UUID) (bool, <-chan StreamDelta, <-chan error, func()) {
	msg, err := s.querier.GetMessageByUser(ctx, GetMessageByUserParams{
		ID:     messageID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil, nil, func() {}
		}
		return true, nil, errorStream(databaseutil.WrapDBErrorWithKeyValue(err, "message", "message_id", messageID.String(), s.logger, "get stream message")), func() {}
	}

	streamEvent, ok := s.streamHub.GetStream(messageID)
	if ok {
		llmCh, errCh, cancel := streamEvent.Subscribe()
		return ok, llmCh, errCh, cancel
	}

	switch MessageStatus(msg.Status) {
	case MessageStatusDone:
		return true, completedStream(msg.Content.String), closedErrorStream(), func() {}
	case MessageStatusError:
		return true, nil, errorStream(fmt.Errorf("message stream failed")), func() {}
	default:
		return true, nil, errorStream(fmt.Errorf("message stream is unavailable")), func() {}
	}
}

func (s *ChatService) streamProcessor(ctx context.Context, chatID uuid.UUID, messageID uuid.UUID, streamEvent *StreamEvent, providerReq CreateChatCompletionRequest, createTitle bool) {
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

	if !endFlag {
		streamEvent.Fail(fmt.Errorf("upstream stream ended before finish"))
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

	// title generate
	if status == MessageStatusDone && createTitle {
		title, err := s.createTitleByLLM(updateCtx, chatID)
		if err != nil {
			SSEError(err, s.logger)
			return
		}
		_, err = s.querier.UpdateChat(updateCtx, UpdateChatParams{
			ID:    chatID,
			Title: title,
		})
		if err != nil {
			SSEError(err, s.logger)
			return
		}
	}

}

func (s *ChatService) ValidatePreviousID(ctx context.Context, userID uuid.UUID, previousID uuid.UUID, chatID uuid.UUID) error {
	if previousID == uuid.Nil {
		return nil
	}
	msg, err := s.querier.GetMessageByUser(ctx, GetMessageByUserParams{
		ID:     previousID,
		UserID: userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return handlerutil.NewNotFoundError("message", "previous_id", previousID.String(), "")
		}
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

func (s *ChatService) createTitleByLLM(ctx context.Context, chatID uuid.UUID) (string, error) {
	messages, err := s.fetchMessages(ctx, chatID)
	if err != nil {
		return "", err
	}
	history := createChatHistory(messages)
	title, err := s.provider.GetTitle(ctx, history)
	if err != nil {
		return "", databaseutil.WrapDBErrorWithKeyValue(err, "title", "chat_id", chatID.String(), s.logger, "get title for chat")
	}
	return title, nil
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

func completedStream(content string) <-chan StreamDelta {
	runes := []rune(content)
	ch := make(chan StreamDelta, len(runes)+1)
	for _, r := range runes {
		ch <- StreamDelta{Delta: string(r), IsFinished: false}
	}
	ch <- StreamDelta{Delta: "", IsFinished: true}
	close(ch)
	return ch
}

func errorStream(err error) <-chan error {
	ch := make(chan error, 1)
	ch <- err
	close(ch)
	return ch
}

func closedErrorStream() <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}
