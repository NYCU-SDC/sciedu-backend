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
	subscribers map[*streamSubscriber]struct{}
	err         error
}

type streamSubscriber struct {
	chunks chan StreamDelta
	errs   chan error
	done   chan struct{}
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

func (s *StreamEvent) Subscribe() (<-chan StreamDelta, <-chan error, func()) {
	s.lock.Lock()
	fullContent := s.fullContent
	status := s.status
	streamErr := s.err
	sub := &streamSubscriber{
		chunks: make(chan StreamDelta, maxInt(64, len([]rune(fullContent))+2)),
		errs:   make(chan error, 1),
		done:   make(chan struct{}),
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

	for _, r := range []rune(fullContent) {
		sub.chunks <- StreamDelta{
			Delta:      string(r),
			IsFinished: false,
		}
	}
	switch status {
	case MessageStatusDone:
		sub.chunks <- StreamDelta{Delta: "", IsFinished: true}
	case MessageStatusError:
		if streamErr != nil {
			sub.errs <- streamErr
		}
	}

	return sub.chunks, sub.errs, cancel
}

func (s *StreamEvent) Publish(stream StreamDelta) {
	s.lock.RLock()
	subs := make([]*streamSubscriber, 0, len(s.subscribers))
	for sub := range s.subscribers {
		subs = append(subs, sub)
	}
	s.lock.RUnlock()

	for _, sub := range subs {
		select {
		case sub.chunks <- stream:
		case <-sub.done:
		}
	}
}

func (s *StreamEvent) AppendDelta(stream StreamDelta) {
	s.lock.Lock()
	s.fullContent += stream.Delta
	s.lock.Unlock()
	for _, r := range []rune(stream.Delta) {
		s.Publish(StreamDelta{
			Delta:      string(r),
			IsFinished: stream.IsFinished,
		})
	}
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

	s.publishError(err)
}

func (s *StreamEvent) Get() (MessageStatus, string, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.status, s.fullContent, s.err
}

func (s *StreamEvent) publishError(err error) {
	s.lock.RLock()
	subs := make([]*streamSubscriber, 0, len(s.subscribers))
	for sub := range s.subscribers {
		subs = append(subs, sub)
	}
	s.lock.RUnlock()

	for _, sub := range subs {
		select {
		case sub.errs <- err:
		case <-sub.done:
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
