package chat

type ChatCompletionChunk struct {
	Delta      string `json:"delta"`
	IsFinished bool   `json:"isFinished"`
}

type ChatRole struct {
	User      string `json:"user"`
	Assistant string `json:"assistant"`
	System    string `json:"system"`
}

type ChatMessage struct {
	Role    ChatRole `json:"role"`
	Content string   `json:"content"`
}

type CreateChatCompletionRequest struct {
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}
