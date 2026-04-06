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

func (this *StreamHub) CreateStream(messageID uuid.UUID) *StreamEvent {
	this.lock.Lock()
	defer this.lock.Unlock()
	if _, ok := this.streams[messageID]; ok {
		return this.streams[messageID]
	}

	stream := &StreamEvent{
		status:      MessageStatusStreaming,
		fullContent: "",
		subscribers: make(map[chan StreamDelta]struct{}),
	}
	this.streams[messageID] = stream
	return stream
}

func (this *StreamHub) GetStream(messageID uuid.UUID) (*StreamEvent, bool) {
	this.lock.RLock()
	defer this.lock.RUnlock()

	stream, ok := this.streams[messageID]
	return stream, ok
}

func (this *StreamHub) DeleteStream(messageID uuid.UUID) {
	this.lock.Lock()
	defer this.lock.Unlock()

	delete(this.streams, messageID)
}

func (this *StreamEvent) Subscribe() (<-chan StreamDelta, <-chan error, func()) {
	ch := make(chan StreamDelta, 16)
	errCh := make(chan error, 1)

	this.lock.Lock()
	this.subscribers[ch] = struct{}{}
	fullContent := this.fullContent
	this.lock.Unlock()

	cancel := func() {
		this.lock.Lock()
		if _, ok := this.subscribers[ch]; ok {
			delete(this.subscribers, ch)
			close(ch)
		}
		this.lock.Unlock()
	}

	ch <- StreamDelta{
		Delta:      fullContent,
		IsFinished: false,
	}

	return ch, errCh, cancel
}

func (this *StreamEvent) Publish(stream StreamDelta) {
	this.lock.RLock()
	subs := make([]chan StreamDelta, 0, len(this.subscribers))
	for ch := range this.subscribers {
		subs = append(subs, ch)
	}
	this.lock.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- stream:
		default: //avoid blocking
		}

	}
}

func (this *StreamEvent) AppendDelta(stream StreamDelta) {
	this.lock.Lock()
	this.fullContent += stream.Delta
	this.lock.Unlock()
	this.Publish(stream)
}

func (this *StreamEvent) Complete() {
	this.lock.Lock()
	this.status = MessageStatusDone
	this.lock.Unlock()
	this.Publish(StreamDelta{
		Delta:      "",
		IsFinished: true,
	})
}

func (this *StreamEvent) Fail(err error) {
	this.lock.Lock()
	this.err = err
	this.status = MessageStatusError
	this.lock.Unlock()

	this.Publish(StreamDelta{
		Delta:      "",
		IsFinished: true,
	})
}

func (this *StreamEvent) Get() (MessageStatus, string, error) {
	this.lock.RLock()
	defer this.lock.RUnlock()
	return this.status, this.fullContent, this.err
}
