package chat

type ChatCompletionChunk struct {
	Delta      string `json:"delta"`
	IsFinished bool   `json:"IsFinished"`
}

type ChatRole struct {
	User      string
	Assistant string
	System    string
}

type ChatMessage struct {
	Role    ChatRole
	Content string
}

type CreateChatCompletionRequest struct {
	Messages []ChatMessage
	Stream   bool
}
