package chat

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestStreamEventAssignsBackendRunIDs(t *testing.T) {
	start := func(agent, parent string) AgentEvent {
		return AgentEvent{Type: AgentEventAgentStart, Agent: agent, Parent: parent, SummonedBy: "summon"}
	}
	end := func(agent string) AgentEvent {
		return AgentEvent{Type: AgentEventAgentEnd, Agent: agent}
	}
	part := func(eventType AgentEventType, index int32, agent string, partType PartType) AgentEvent {
		return AgentEvent{Type: eventType, Index: &index, Part: &MessagePart{
			Type: partType, ID: "part-" + agent, Agent: agent, AgentRunID: "provider-owned-id",
		}}
	}

	tests := []struct {
		name        string
		events      []AgentEvent
		wantAgents  []string
		wantParents []int
		wantParts   []int
	}{
		{
			name: "repeated agent calls and a run without parts",
			events: []AgentEvent{
				start("teacher", ""),
				part(AgentEventPartStart, 0, "teacher", PartTypeText),
				part(AgentEventPartEnd, 0, "teacher", PartTypeText), end("teacher"),
				start("teacher", ""), end("teacher"),
				start("teacher", ""),
				part(AgentEventPartStart, 1, "teacher", PartTypeToolCall),
				part(AgentEventPartEnd, 1, "teacher", PartTypeToolCall), end("teacher"),
			},
			wantAgents: []string{"teacher", "teacher", "teacher"}, wantParents: []int{-1, -1, -1}, wantParts: []int{0, 2},
		},
		{
			name: "nested recursive agent uses nearest active parent and resumes outer run",
			events: []AgentEvent{
				start("teacher", ""),
				part(AgentEventPartStart, 0, "teacher", PartTypeText),
				part(AgentEventPartEnd, 0, "teacher", PartTypeText),
				start("teacher", "teacher"),
				part(AgentEventPartStart, 1, "teacher", PartTypeReasoning),
				part(AgentEventPartEnd, 1, "teacher", PartTypeReasoning),
				start("student", "teacher"),
				part(AgentEventPartStart, 2, "student", PartTypeToolResult),
				part(AgentEventPartEnd, 2, "student", PartTypeToolResult), end("student"), end("teacher"),
				part(AgentEventPartStart, 3, "teacher", PartTypeText),
				part(AgentEventPartEnd, 3, "teacher", PartTypeText), end("teacher"),
			},
			wantAgents: []string{"teacher", "teacher", "student"}, wantParents: []int{-1, 0, 1}, wantParts: []int{0, 1, 2, 0},
		},
		{
			name: "late part end retains original run rather than the new invocation",
			events: []AgentEvent{
				start("teacher", ""), part(AgentEventPartStart, 0, "teacher", PartTypeText), end("teacher"),
				start("teacher", ""), part(AgentEventPartEnd, 0, "teacher", PartTypeText), end("teacher"),
			},
			wantAgents: []string{"teacher", "teacher"}, wantParents: []int{-1, -1}, wantParts: []int{0},
		},
		{
			name: "part end without start uses active invocation",
			events: []AgentEvent{
				start("teacher", "missing"), part(AgentEventPartEnd, 0, "teacher", PartTypeText), end("teacher"),
			},
			wantAgents: []string{"teacher"}, wantParents: []int{-1}, wantParts: []int{0},
		},
		{
			name: "parts without invocation stay unlinked",
			events: []AgentEvent{
				part(AgentEventPartStart, 0, "teacher", PartTypeText),
				part(AgentEventPartEnd, 0, "teacher", PartTypeText),
			},
			wantParts: []int{-1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := NewStreamHub().CreateStream(uuid.New())
			live, _, cancel := stream.Subscribe()
			defer cancel()
			for _, event := range tt.events {
				require.NoError(t, stream.Apply(agentChunk(event)))
			}
			_, _, data, _, err := stream.Get()
			require.NoError(t, err)
			require.Len(t, data.AgentRuns, len(tt.wantAgents))
			ids := make(map[string]bool)
			for i, run := range data.AgentRuns {
				_, err := uuid.Parse(run.ID)
				require.NoError(t, err)
				require.False(t, ids[run.ID], "each invocation needs its own ID")
				ids[run.ID] = true
				require.Equal(t, tt.wantAgents[i], run.Agent)
				require.Equal(t, "summon", run.SummonedBy)
				if parent := tt.wantParents[i]; parent >= 0 {
					require.Equal(t, data.AgentRuns[parent].ID, run.ParentRunID)
				} else {
					require.Empty(t, run.ParentRunID)
				}
			}
			require.Len(t, data.Parts, len(tt.wantParts))
			for i, runIndex := range tt.wantParts {
				var wantID string
				if runIndex >= 0 {
					wantID = data.AgentRuns[runIndex].ID
				}
				require.Equal(t, wantID, data.Parts[i].AgentRunID)
			}
			reconnected, _, cancelReplay := stream.Subscribe()
			defer cancelReplay()
			for _, event := range tt.events {
				liveChunk := receiveStreamChunk(t, live)
				require.Equal(t, liveChunk, receiveStreamChunk(t, reconnected))
				if liveChunk.Agentic.Part != nil {
					require.Equal(t, data.Parts[*event.Index].AgentRunID, liveChunk.Agentic.Part.AgentRunID)
					require.Equal(t, "provider-owned-id", event.Part.AgentRunID, "do not mutate provider events")
				}
			}
			if len(data.AgentRuns) > 0 {
				data.AgentRuns[0].ID = "changed"
				_, _, next, _, _ := stream.Get()
				require.NotEqual(t, "changed", next.AgentRuns[0].ID, "returned runs must not alias hub state")
			}
		})
	}
}

func TestCompletedAgenticStreamPreservesInvocationSnapshots(t *testing.T) {
	tests := []struct {
		name      string
		runs      []AgentRun
		partRuns  []string
		wantOrder []string
	}{
		{
			name:      "separate runs of the same character including an empty run",
			runs:      []AgentRun{{ID: "r0", Agent: "teacher"}, {ID: "r1", Agent: "teacher"}, {ID: "r2", Agent: "teacher"}},
			partRuns:  []string{"r0", "r2"},
			wantOrder: []string{"start:teacher", "part:r0", "end:teacher", "start:teacher", "end:teacher", "start:teacher", "part:r2", "end:teacher"},
		},
		{
			name:      "nested agent and resumed parent",
			runs:      []AgentRun{{ID: "r0", Agent: "teacher"}, {ID: "r1", Agent: "student", ParentRunID: "r0", SummonedBy: "summon_student"}},
			partRuns:  []string{"r0", "r1", "r0"},
			wantOrder: []string{"start:teacher", "part:r0", "start:student", "part:r1", "end:student", "part:r0", "end:teacher"},
		},
		{
			name:      "invocations with no parts are retained",
			runs:      []AgentRun{{ID: "r0", Agent: "teacher"}, {ID: "r1", Agent: "student", ParentRunID: "r0"}},
			wantOrder: []string{"start:teacher", "start:student", "end:student", "end:teacher"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := AgenticData{AgentRuns: tt.runs, FinishReason: "stop"}
			for _, id := range tt.partRuns {
				for _, run := range tt.runs {
					if run.ID == id {
						data.Parts = append(data.Parts, MessagePart{Type: PartTypeText, ID: "part-" + id, Agent: run.Agent, AgentRunID: id, Text: "text"})
					}
				}
			}
			encoded, err := json.Marshal(data)
			require.NoError(t, err)
			var saved AgenticData
			require.NoError(t, json.Unmarshal(encoded, &saved))
			for range 2 {
				var order []string
				var starts []AgentEvent
				var finalParts []MessagePart
				for chunk := range completedAgenticStream(saved) {
					event := *chunk.Agentic
					require.NotEqual(t, AgentEventDelta, event.Type)
					switch event.Type {
					case AgentEventAgentStart:
						order = append(order, "start:"+event.Agent)
						starts = append(starts, event)
					case AgentEventAgentEnd:
						order = append(order, "end:"+event.Agent)
					case AgentEventPartStart:
						require.Equal(t, saved.Parts[*event.Index].AgentRunID, event.Part.AgentRunID)
					case AgentEventPartEnd:
						order = append(order, "part:"+event.Part.AgentRunID)
						finalParts = append(finalParts, *event.Part)
					case AgentEventDone:
						require.Equal(t, "stop", event.FinishReason)
					}
				}
				require.Equal(t, tt.wantOrder, order)
				require.Equal(t, saved.Parts, finalParts)
				require.Len(t, starts, len(tt.runs))
				for i, run := range tt.runs {
					require.Equal(t, run.Agent, starts[i].Agent)
					require.Equal(t, run.SummonedBy, starts[i].SummonedBy)
					if run.ParentRunID != "" {
						require.Equal(t, "teacher", starts[i].Parent)
					}
				}
			}
		})
	}
}

func TestVisibleTextConcatenatesOnlyPublicText(t *testing.T) {
	tests := []struct {
		name  string
		parts []MessagePart
		want  string
	}{
		{name: "empty"},
		{name: "no added separators", parts: []MessagePart{
			{Type: PartTypeText, Text: "光合"},
			{Type: PartTypeReasoning, Text: "reasoning"},
			{Type: PartTypeText, Text: "hidden", Internal: true},
			{Type: PartTypeToolResult, Text: "tool"},
			{Type: PartTypeText, Text: "作用\n"},
			{Type: PartTypeText, Text: "保留換行"},
		}, want: "光合作用\n保留換行"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, visibleText(tt.parts))
		})
	}
}
