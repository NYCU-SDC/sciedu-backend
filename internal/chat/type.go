package chat

type StreamDelta struct {
	Delta      string `json:"delta"`
	IsFinished bool   `json:"isFinished"`
}

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
)

type MessageStatus string

const (
	MessageStatusStreaming MessageStatus = "streaming"
	MessageStatusDone      MessageStatus = "completed"
	MessageStatusError     MessageStatus = "failed"
)

type ChatMessage struct {
	Role    MessageRole `json:"role"`
	Content string      `json:"content"`
}

type CreateChatCompletionRequest struct {
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type ChatPage struct {
	Items       []ChatReturn `json:"items"`
	TotalPages  int32        `json:"totalPages"`
	TotalItems  int32        `json:"totalItems"`
	CurrentPage int32        `json:"currentPage"`
	PageSize    int32        `json:"pageSize"`
	HasNextPage bool         `json:"hasNextPage"`
}

const (
	defaultPage     int32 = 1
	defaultPageSize int32 = 20
	maxPageSize     int32 = 100
)
