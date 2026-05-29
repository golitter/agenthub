package stream

import (
	"sync"
	"sync/atomic"
)

// HubEvent is delivered to subscribers via channels.
type HubEvent struct {
	Seq  uint64
	Data string // raw SSE line, e.g. `data: {"type":"text",...}`
	Done bool   // true when the stream is closing
}

// subscriber wraps a per-subscriber buffered channel.
type subscriber struct {
	ch chan HubEvent
}

// RuntimeStream tracks a single active stream with its own seq counter.
type RuntimeStream struct {
	seq         atomic.Uint64
	mu          sync.Mutex
	subscribers map[*subscriber]struct{}
	closed      bool
}

// RuntimeHub is a singleton in-memory pub/sub hub.
// Key format: "sessionID:messageID" (same as Redis stream key suffix).
type RuntimeHub struct {
	mu      sync.RWMutex
	streams map[string]*RuntimeStream
}

// Hub is the global RuntimeHub instance.
var Hub = &RuntimeHub{
	streams: make(map[string]*RuntimeStream),
}

func (h *RuntimeHub) getOrCreateStream(key string) *RuntimeStream {
	h.mu.RLock()
	s, ok := h.streams[key]
	h.mu.RUnlock()
	if ok {
		return s
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	// double-check after acquiring write lock
	s, ok = h.streams[key]
	if ok {
		return s
	}
	s = &RuntimeStream{
		subscribers: make(map[*subscriber]struct{}),
	}
	h.streams[key] = s
	return s
}

// Publish sends an event to all subscribers of the given stream key.
// If the stream does not exist, it is created. Publish is non-blocking;
// if a subscriber's buffer is full, the event is silently dropped.
func (h *RuntimeHub) Publish(key, data string) {
	s := h.getOrCreateStream(key)
	seq := s.seq.Add(1)
	evt := HubEvent{Seq: seq, Data: data}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	for sub := range s.subscribers {
		select {
		case sub.ch <- evt:
		default:
			// buffer full — drop oldest: drain one, push new
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- evt:
			default:
			}
		}
	}
}

// Subscribe returns a channel for consuming events and the current sequence number.
// The caller uses currentSeq to replay any Redis gap before consuming live events.
func (h *RuntimeHub) Subscribe(key string) (<-chan HubEvent, uint64) {
	s := h.getOrCreateStream(key)

	sub := &subscriber{
		ch: make(chan HubEvent, 1024),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	currentSeq := s.seq.Load()
	if !s.closed {
		s.subscribers[sub] = struct{}{}
	}
	return sub.ch, currentSeq
}

// Close marks the stream as done, sends a Done event to all subscribers,
// closes their channels, and removes the stream from the hub.
func (h *RuntimeHub) Close(key string) {
	h.mu.Lock()
	s, ok := h.streams[key]
	if ok {
		delete(h.streams, key)
	}
	h.mu.Unlock()

	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for sub := range s.subscribers {
		// send Done sentinel
		select {
		case sub.ch <- HubEvent{Done: true}:
		default:
		}
		close(sub.ch)
		delete(s.subscribers, sub)
	}
}
