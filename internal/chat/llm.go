package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type LLMClient struct {
	baseURL    string
	httpClient *http.Client
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmRequest struct {
	Model    *string      `json:"model,omitempty"`
	Stream   bool         `json:"stream"`
	Messages []llmMessage `json:"messages"`
}

type llmChunk struct {
	Delta      string `json:"delta"`
	IsFinished bool   `json:"isFinished"`
}

func NewLLMClient(baseURL string, httpClient *http.Client) *LLMClient {
	return &LLMClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *LLMClient) Stream(ctx context.Context, messages []Message) (<-chan string, <-chan error) {
	chunks := make(chan string)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		body, err := json.Marshal(llmRequest{Stream: true, Messages: toLLMMessages(messages)})
		if err != nil {
			errs <- err
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat", bytes.NewReader(body))
		if err != nil {
			errs <- err
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			errs <- err
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			errs <- fmt.Errorf("llm module returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			line = strings.TrimPrefix(line, "data:")
			line = strings.TrimSpace(line)
			if line == "[DONE]" {
				return
			}
			var chunk llmChunk
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				errs <- err
				return
			}
			if chunk.Delta != "" {
				chunks <- chunk.Delta
			}
			if chunk.IsFinished {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errs <- err
		}
	}()

	return chunks, errs
}

func toLLMMessages(messages []Message) []llmMessage {
	llmMessages := make([]llmMessage, 0, len(messages)+1)
	llmMessages = append(llmMessages, llmMessage{
		Role:    "developer",
		Content: "You are a helpful science education assistant. Answer clearly and encourage learning.",
	})
	for _, message := range messages {
		if message.Status == StatusStreaming || message.Content == "" {
			continue
		}
		llmMessages = append(llmMessages, llmMessage{
			Role:    string(message.Role),
			Content: message.Content,
		})
	}
	return llmMessages
}
