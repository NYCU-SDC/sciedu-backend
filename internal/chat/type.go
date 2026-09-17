package chat

import (
	"encoding/json"
	"fmt"
)

type StreamDelta struct {
	Delta      string `json:"delta"`
	IsFinished bool   `json:"isFinished"`
}

type AgentEventType string

const (
	AgentEventCast       AgentEventType = "cast"
	AgentEventPartStart  AgentEventType = "part_start"
	AgentEventDelta      AgentEventType = "delta"
	AgentEventPartEnd    AgentEventType = "part_end"
	AgentEventAgentStart AgentEventType = "agent_start"
	AgentEventAgentEnd   AgentEventType = "agent_end"
	AgentEventDone       AgentEventType = "done"
	AgentEventError      AgentEventType = "error"
)

type PartType string

const (
	PartTypeText       PartType = "text"
	PartTypeReasoning  PartType = "reasoning"
	PartTypeToolCall   PartType = "tool_call"
	PartTypeToolResult PartType = "tool_result"
)

type Character struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

type MessagePart struct {
	Type       PartType        `json:"type"`
	ID         string          `json:"id"`
	Agent      string          `json:"agent"`
	Internal   bool            `json:"internal,omitempty"`
	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Status     string          `json:"status,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
}

type AgentEvent struct {
	Type         AgentEventType `json:"type"`
	Characters   []Character    `json:"characters,omitempty"`
	Index        *int32         `json:"index,omitempty"`
	Part         *MessagePart   `json:"part,omitempty"`
	Delta        string         `json:"delta,omitempty"`
	Agent        string         `json:"agent,omitempty"`
	Parent       string         `json:"parent,omitempty"`
	SummonedBy   string         `json:"summonedBy,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
	Status       MessageStatus  `json:"status,omitempty"`
	Error        string         `json:"error,omitempty"`
	Code         string         `json:"code,omitempty"`
}

type StreamChunk struct {
	Legacy  *StreamDelta `json:"-"`
	Agentic *AgentEvent  `json:"-"`
}

func (c StreamChunk) MarshalJSON() ([]byte, error) {
	switch {
	case c.Agentic != nil:
		return json.Marshal(c.Agentic)
	case c.Legacy != nil:
		return json.Marshal(c.Legacy)
	default:
		return nil, fmt.Errorf("empty stream chunk")
	}
}

func (c StreamChunk) Terminal() bool {
	if c.Agentic != nil {
		return c.Agentic.Type == AgentEventDone || c.Agentic.Type == AgentEventError
	}
	return c.Legacy != nil && c.Legacy.IsFinished
}

type AgenticData struct {
	Cast         []Character   `json:"cast,omitempty"`
	Parts        []MessagePart `json:"parts"`
	FinishReason string        `json:"finishReason,omitempty"`
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

type AgentsRequest struct {
	Messages []ChatMessage `json:"messages"`
	Preset   string        `json:"preset,omitempty"`
	Session  string        `json:"session,omitempty"`
	Stream   bool          `json:"stream"`
	User     string        `json:"user,omitempty"`
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
