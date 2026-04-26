package chat

import (
	"sync"

	"github.com/google/uuid"
)

type StreamHub struct {
	lock    sync.RWMutex
	streams map[uuid.UUID]*StreamEvent
}

func NewStreamHub() *StreamHub {
	return &StreamHub{
		streams: make(map[uuid.UUID]*StreamEvent),
	}
}

type StreamEvent struct {
	lock        sync.RWMutex
	status      MessageStatus
	fullContent string
	subscribers map[chan StreamDelta]struct{}
	err         error
}

func (s *StreamHub) CreateStream(messageID uuid.UUID) *StreamEvent {
	s.lock.Lock()
	defer s.lock.Unlock()
	if _, ok := s.streams[messageID]; ok {
		return s.streams[messageID]
	}

	stream := &StreamEvent{
		status:      MessageStatusStreaming,
		fullContent: "",
		subscribers: make(map[chan StreamDelta]struct{}),
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

func (s *StreamEvent) Subscribe() (<-chan StreamDelta, <-chan error, func()) {
	ch := make(chan StreamDelta, 16)
	errCh := make(chan error, 1)

	s.lock.Lock()
	s.subscribers[ch] = struct{}{}
	fullContent := s.fullContent
	s.lock.Unlock()

	cancel := func() {
		s.lock.Lock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
		s.lock.Unlock()
	}

	ch <- StreamDelta{
		Delta:      fullContent,
		IsFinished: false,
	}

	return ch, errCh, cancel
}

func (s *StreamEvent) Publish(stream StreamDelta) {
	s.lock.RLock()
	subs := make([]chan StreamDelta, 0, len(s.subscribers))
	for ch := range s.subscribers {
		subs = append(subs, ch)
	}
	s.lock.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- stream:
		default: //avoid blocking
		}

	}
}

func (s *StreamEvent) AppendDelta(stream StreamDelta) {
	s.lock.Lock()
	s.fullContent += stream.Delta
	s.lock.Unlock()
	s.Publish(stream)
}

func (s *StreamEvent) Complete() {
	s.lock.Lock()
	s.status = MessageStatusDone
	s.lock.Unlock()
	s.Publish(StreamDelta{
		Delta:      "",
		IsFinished: true,
	})
}

func (s *StreamEvent) Fail(err error) {
	s.lock.Lock()
	s.err = err
	s.status = MessageStatusError
	s.lock.Unlock()

	s.Publish(StreamDelta{
		Delta:      "",
		IsFinished: true,
	})
}

func (s *StreamEvent) Get() (MessageStatus, string, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.status, s.fullContent, s.err
}
