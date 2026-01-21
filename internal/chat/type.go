package chat

type ChatCompletionChunk struct {
	Delta      string `json:"delta"`
	IsFinished bool   `json:"isFinished"`
}

type ChatRole string

const (
	ChatRoleUser      ChatRole = "user"
	ChatRoleAssistant ChatRole = "assistant"
	ChatRoleSystem    ChatRole = "system"
)

type ChatMessage struct {
	Role    ChatRole `json:"role"`
	Content string   `json:"content"`
}

type CreateChatCompletionRequest struct {
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}



