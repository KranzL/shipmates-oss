package goshared

import "sync"

type ClarificationBus struct {
	mu        sync.Mutex
	listeners map[string][]chan string
}

func NewClarificationBus() *ClarificationBus {
	return &ClarificationBus{
		listeners: make(map[string][]chan string),
	}
}

func (b *ClarificationBus) Subscribe(clarificationID string) chan string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan string, 1)
	b.listeners[clarificationID] = append(b.listeners[clarificationID], ch)
	return ch
}

func (b *ClarificationBus) Publish(clarificationID string, answer string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.listeners[clarificationID] {
		select {
		case ch <- answer:
		default:
		}
	}
	delete(b.listeners, clarificationID)
}

func (b *ClarificationBus) Unsubscribe(clarificationID string, ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	listeners := b.listeners[clarificationID]
	filtered := make([]chan string, 0, len(listeners))
	for _, l := range listeners {
		if l != ch {
			filtered = append(filtered, l)
		}
	}
	if len(filtered) == 0 {
		delete(b.listeners, clarificationID)
	} else {
		b.listeners[clarificationID] = filtered
	}
}
