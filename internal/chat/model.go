package chat

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Status string

const (
	StatusCreated   Status = "created"
	StatusStreaming Status = "streaming"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

type Message struct {
	ID         uuid.UUID  `json:"id"`
	Content    string     `json:"content"`
	Role       Role       `json:"role"`
	PreviousID *uuid.UUID `json:"previousID,omitempty"`
	Status     Status     `json:"status"`
	ChatID     uuid.UUID  `json:"-"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type CreateChatResponse struct {
	ChatID uuid.UUID `json:"chatID"`
}

type MessagesResponse struct {
	Messages []Message `json:"messages"`
}

type SendMessageRequest struct {
	Content    string     `json:"content"`
	PreviousID *uuid.UUID `json:"previousID"`
}

type SendMessageResponse struct {
	Message        Message   `json:"message"`
	ReplyMessageID uuid.UUID `json:"replyMessageID"`
}

type StreamChunk struct {
	Content string `json:"content"`
}

type StreamError struct {
	Error string `json:"error"`
}

type StreamEvent struct {
	Type    string
	Content string
	Err     error
}

const (
	StreamEventDelta = "delta"
	StreamEventDone  = "done"
	StreamEventError = "error"
)
