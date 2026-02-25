package relay

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Hub struct {
	mu          sync.Mutex
	sessions    map[string]map[*Subscriber]struct{}
	sequenceMap sync.Map
	maxConns    int64
	activeConns int64
	subPool     sync.Pool
}

func NewHub(maxConns int64) *Hub {
	h := &Hub{
		sessions: make(map[string]map[*Subscriber]struct{}),
		maxConns: maxConns,
	}
	h.subPool = sync.Pool{
		New: func() any {
			s := make([]*Subscriber, 0, 16)
			return &s
		},
	}
	return h
}

func (h *Hub) Subscribe(sessionID string) (*Subscriber, error) {
	h.mu.Lock()
	if atomic.LoadInt64(&h.activeConns) >= h.maxConns {
		h.mu.Unlock()
		return nil, fmt.Errorf("max connections reached")
	}
	atomic.AddInt64(&h.activeConns, 1)
	sub := NewSubscriber(256)
	subs, ok := h.sessions[sessionID]
	if !ok {
		subs = make(map[*Subscriber]struct{})
		h.sessions[sessionID] = subs
	}
	subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub, nil
}

func (h *Hub) Unsubscribe(sessionID string, sub *Subscriber) {
	h.mu.Lock()
	if subs, ok := h.sessions[sessionID]; ok {
		if _, exists := subs[sub]; exists {
			delete(subs, sub)
			atomic.AddInt64(&h.activeConns, -1)
		}
		if len(subs) == 0 {
			delete(h.sessions, sessionID)
		}
	}
	h.mu.Unlock()
	sub.Close()
}

func (h *Hub) Publish(sessionID string, data []byte) {
	h.mu.Lock()
	subscriberSlice := h.subPool.Get().(*[]*Subscriber)
	subs := (*subscriberSlice)[:0]
	for sub := range h.sessions[sessionID] {
		subs = append(subs, sub)
	}
	h.mu.Unlock()

	for _, sub := range subs {
		select {
		case <-sub.Done:
			continue
		default:
		}
		select {
		case <-sub.Done:
		case sub.Send <- data:
		default:
			select {
			case <-sub.Done:
			default:
				sub.IncrementDrops()
				h.sendDropMarker(sub, sessionID)
			}
		}
	}

	*subscriberSlice = subs[:0]
	h.subPool.Put(subscriberSlice)
}

func (h *Hub) sendDropMarker(sub *Subscriber, sessionID string) {
	marker := buildDropMarker(sessionID)
	select {
	case <-sub.Done:
	case sub.Send <- marker:
	default:
	}
}

func (h *Hub) NextSequence(sessionID string) int {
	val, _ := h.sequenceMap.LoadOrStore(sessionID, &atomic.Int64{})
	counter := val.(*atomic.Int64)
	return int(counter.Add(1))
}

func (h *Hub) CleanupSequence(sessionID string) {
	h.sequenceMap.Delete(sessionID)
}

func (h *Hub) SubscriberCount(sessionID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions[sessionID])
}

func (h *Hub) TotalSubscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	total := 0
	for _, subs := range h.sessions {
		total += len(subs)
	}
	return total
}

func (h *Hub) ActiveConnections() int64 {
	return atomic.LoadInt64(&h.activeConns)
}

func (h *Hub) CleanupOrphanedSequences() {
	h.sequenceMap.Range(func(key, _ any) bool {
		sessionID := key.(string)
		h.mu.Lock()
		count := len(h.sessions[sessionID])
		h.mu.Unlock()
		if count == 0 {
			h.sequenceMap.Delete(sessionID)
		}
		return true
	})
}

func (h *Hub) StartSequenceCleanup(interval time.Duration, done <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.CleanupOrphanedSequences()
			case <-done:
				return
			}
		}
	}()
}
