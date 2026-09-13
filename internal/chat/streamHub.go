package chat

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

var errSlowStreamSubscriber = errors.New("stream subscriber is too slow")

type StreamHub struct {
	lock    sync.RWMutex
	streams map[uuid.UUID]*StreamEvent
}

func NewStreamHub() *StreamHub {
	return &StreamHub{streams: make(map[uuid.UUID]*StreamEvent)}
}

type StreamEvent struct {
	lock        sync.RWMutex
	status      MessageStatus
	fullContent string
	agentic     bool
	agenticData AgenticData
	parts       map[int32]MessagePart
	partDeltas  map[int32]string
	history     []StreamChunk
	subscribers map[*streamSubscriber]struct{}
	err         error
}

type streamSubscriber struct {
	chunks chan StreamChunk
	errs   chan error
	done   chan struct{}
}

func (s *StreamHub) CreateStream(messageID uuid.UUID) *StreamEvent {
	s.lock.Lock()
	defer s.lock.Unlock()
	if stream, ok := s.streams[messageID]; ok {
		return stream
	}

	stream := &StreamEvent{
		status:      MessageStatusStreaming,
		parts:       make(map[int32]MessagePart),
		partDeltas:  make(map[int32]string),
		subscribers: make(map[*streamSubscriber]struct{}),
	}
	s.streams[messageID] = stream
	return stream
}

func (s *StreamHub) GetStream(messageID uuid.UUID) (*StreamEvent, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	stream, ok := s.streams[messageID]
	return stream, ok
}

func (s *StreamHub) DeleteStream(messageID uuid.UUID) {
	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.streams, messageID)
}

func (s *StreamEvent) Subscribe() (<-chan StreamChunk, <-chan error, func()) {
	s.lock.Lock()
	sub := &streamSubscriber{
		chunks: make(chan StreamChunk, maxInt(64, len(s.history)+2)),
		errs:   make(chan error, 1),
		done:   make(chan struct{}),
	}
	for _, chunk := range s.history {
		sub.chunks <- chunk
	}
	if s.status == MessageStatusError && s.err != nil {
		sub.errs <- s.err
	}
	s.subscribers[sub] = struct{}{}
	s.lock.Unlock()

	cancel := func() {
		s.lock.Lock()
		if _, ok := s.subscribers[sub]; ok {
			delete(s.subscribers, sub)
			close(sub.done)
		}
		s.lock.Unlock()
	}
	return sub.chunks, sub.errs, cancel
}

func (s *StreamEvent) Apply(chunk StreamChunk) error {
	if chunk.Agentic == nil && chunk.Legacy == nil {
		return errors.New("empty stream chunk")
	}

	s.lock.Lock()
	s.history = append(s.history, chunk)
	if chunk.Agentic != nil {
		s.applyAgentEventLocked(*chunk.Agentic)
	} else {
		s.fullContent += chunk.Legacy.Delta
		if chunk.Legacy.IsFinished {
			s.status = MessageStatusDone
		}
	}
	subs := make([]*streamSubscriber, 0, len(s.subscribers))
	for sub := range s.subscribers {
		subs = append(subs, sub)
	}
	s.lock.Unlock()

	for _, sub := range subs {
		select {
		case sub.chunks <- chunk:
		case <-sub.done:
		default:
			s.disconnectSlowSubscriber(sub)
		}
	}
	return nil
}

func (s *StreamEvent) applyAgentEventLocked(event AgentEvent) {
	s.agentic = true
	switch event.Type {
	case AgentEventCast:
		s.agenticData.Cast = append([]Character(nil), event.Characters...)
	case AgentEventPartStart:
		if event.Index != nil && event.Part != nil {
			s.parts[*event.Index] = *event.Part
		}
	case AgentEventDelta:
		if event.Index != nil {
			s.partDeltas[*event.Index] += event.Delta
		}
	case AgentEventPartEnd:
		if event.Index != nil && event.Part != nil {
			s.parts[*event.Index] = *event.Part
			delete(s.partDeltas, *event.Index)
		}
	case AgentEventDone:
		s.agenticData.FinishReason = event.FinishReason
		s.status = MessageStatusDone
	case AgentEventError:
		s.status = MessageStatusError
	}
	s.agenticData.Parts = s.partsLocked()
	s.fullContent = visibleText(s.agenticData.Parts)
}

func (s *StreamEvent) partsLocked() []MessagePart {
	indices := make([]int, 0, len(s.parts))
	for index := range s.parts {
		indices = append(indices, int(index))
	}
	sort.Ints(indices)

	parts := make([]MessagePart, 0, len(indices))
	for _, rawIndex := range indices {
		index := int32(rawIndex)
		part := s.parts[index]
		if delta := s.partDeltas[index]; delta != "" {
			applyPartDelta(&part, delta)
		}
		parts = append(parts, part)
	}
	return parts
}

func applyPartDelta(part *MessagePart, delta string) {
	switch part.Type {
	case PartTypeText, PartTypeReasoning:
		part.Text = delta
	case PartTypeToolCall:
		if json.Valid([]byte(delta)) {
			part.Arguments = json.RawMessage(delta)
		}
	case PartTypeToolResult:
		if json.Valid([]byte(delta)) {
			part.Content = json.RawMessage(delta)
		}
	}
}

func visibleText(parts []MessagePart) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type == PartTypeText && !part.Internal && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func (s *StreamEvent) Fail(err error) {
	s.lock.Lock()
	s.err = err
	s.status = MessageStatusError
	subs := make([]*streamSubscriber, 0, len(s.subscribers))
	for sub := range s.subscribers {
		subs = append(subs, sub)
	}
	s.lock.Unlock()

	for _, sub := range subs {
		select {
		case sub.errs <- err:
		case <-sub.done:
		default:
		}
	}
}

func (s *StreamEvent) Get() (MessageStatus, string, AgenticData, bool, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	data := s.agenticData
	data.Cast = append([]Character(nil), data.Cast...)
	data.Parts = s.partsLocked()
	return s.status, s.fullContent, data, s.agentic, s.err
}

func (s *StreamEvent) disconnectSlowSubscriber(sub *streamSubscriber) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if _, ok := s.subscribers[sub]; !ok {
		return
	}
	delete(s.subscribers, sub)
	select {
	case sub.errs <- errSlowStreamSubscriber:
	default:
	}
	close(sub.done)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
