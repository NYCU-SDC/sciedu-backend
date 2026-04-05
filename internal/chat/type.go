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
	MessageStatusDone      MessageStatus = "done"
	MessageStatusError     MessageStatus = "error"
)

type ChatMessage struct {
	Role    MessageRole `json:"role"`
	Content string      `json:"content"`
}

type CreateChatCompletionRequest struct {
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}
