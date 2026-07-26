package stream

import (
	"sync"
	"sync/atomic"
	"time"
)

// HubEvent 通过 channel 分发给订阅者。
type HubEvent struct {
	Seq  uint64
	Data string // 原始 SSE 行，例如 `data: {"type":"text",...}`
	Done bool   // 为 true 表示流即将关闭
}

// subscriber 封装一个针对单个订阅者的缓冲 channel。
type subscriber struct {
	ch chan HubEvent
}

// RuntimeStream 跟踪单个活跃流，并维护自己的 seq 计数器。
type RuntimeStream struct {
	seq         atomic.Uint64
	mu          sync.Mutex
	subscribers map[*subscriber]struct{}
	closed      bool
}

// RuntimeHub 是单例的内存发布/订阅中心。
// key 格式："sessionID:messageID"（与 Redis stream key 的后缀一致）。
type RuntimeHub struct {
	mu         sync.RWMutex
	streams    map[string]*RuntimeStream
	closedKeys map[string]struct{} // 已关闭的 key 集合，防止再次创建
}

// Hub 是全局的 RuntimeHub 实例。
var Hub = &RuntimeHub{
	streams:    make(map[string]*RuntimeStream),
	closedKeys: make(map[string]struct{}),
}

func (h *RuntimeHub) getOrCreateStream(key string) *RuntimeStream {
	h.mu.RLock()
	s, ok := h.streams[key]
	_, closed := h.closedKeys[key]
	h.mu.RUnlock()
	if ok {
		return s
	}
	if closed {
		return nil // 流已关闭，不再重建
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	// 拿到写锁后再次检查（double-check）
	s, ok = h.streams[key]
	if ok {
		return s
	}
	if _, closed = h.closedKeys[key]; closed {
		return nil
	}
	s = &RuntimeStream{
		subscribers: make(map[*subscriber]struct{}),
	}
	h.streams[key] = s
	return s
}

// Publish 将事件发送给指定 stream key 的所有订阅者。
// 若该流尚不存在，则创建之。Publish 是非阻塞的：当某个订阅者的缓冲已满时，事件会被静默丢弃。
// 若该 stream key 此前已被关闭，则事件被静默丢弃。
func (h *RuntimeHub) Publish(key, data string) {
	s := h.getOrCreateStream(key)
	if s == nil {
		return // 流已关闭，丢弃事件
	}
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
			// 缓冲已满——丢弃最旧的一条：先排掉一个，再塞入新的
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

// Subscribe 返回一个用于消费事件的 channel，以及当前的序列号。
// 调用方用 currentSeq 先回放 Redis 中遗漏的间隙，再消费实时事件。
// 若该 stream key 已被关闭，则返回 nil channel。
func (h *RuntimeHub) Subscribe(key string) (<-chan HubEvent, uint64) {
	s := h.getOrCreateStream(key)
	if s == nil {
		return nil, 0
	}

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

// Unsubscribe 从流中移除某个订阅者的 channel。
// 在 SSE 客户端断开时调用，以避免 goroutine/channel 泄漏。
func (h *RuntimeHub) Unsubscribe(key string, ch <-chan HubEvent) {
	h.mu.RLock()
	s, ok := h.streams[key]
	h.mu.RUnlock()
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.subscribers {
		if sub.ch == ch {
			delete(s.subscribers, sub)
			return
		}
	}
}

// Close 将流标记为结束，向所有订阅者发送 Done 事件，
// 关闭它们的 channel，并把该流从 hub 中移除。
// 该 key 会被记入 closedKeys，防止再次被创建。
func (h *RuntimeHub) Close(key string) {
	h.mu.Lock()
	s, ok := h.streams[key]
	if ok {
		delete(h.streams, key)
	}
	h.closedKeys[key] = struct{}{} // 标记为已终结
	h.mu.Unlock()

	if !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for sub := range s.subscribers {
		// 发送 Done 哨兵事件
		select {
		case sub.ch <- HubEvent{Done: true}:
		default:
		}
		close(sub.ch)
		delete(s.subscribers, sub)
	}
}

// StartClosedKeysCleanup 启动一个后台 goroutine，定期重置 closedKeys map。
// 这些条目只需存活到「阻止流式传输过程中再次创建」即可；10 分钟之后便不再有意义。
func (h *RuntimeHub) StartClosedKeysCleanup() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			h.mu.Lock()
			h.closedKeys = make(map[string]struct{})
			h.mu.Unlock()
		}
	}()
}
