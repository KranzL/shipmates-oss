package relay

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

type Chunk struct {
	SessionID string `json:"sessionId"`
	Sequence  int    `json:"sequence"`
	Content   string `json:"content"`
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Dropped   int64  `json:"dropped,omitempty"`
}

type Subscriber struct {
	Send      chan []byte
	Done      chan struct{}
	closeOnce sync.Once
	drops     atomic.Int64
}

func NewSubscriber(bufferSize int) *Subscriber {
	return &Subscriber{
		Send: make(chan []byte, bufferSize),
		Done: make(chan struct{}),
	}
}

func (s *Subscriber) Close() {
	s.closeOnce.Do(func() {
		close(s.Done)
	})
}

func (s *Subscriber) IsClosed() bool {
	select {
	case <-s.Done:
		return true
	default:
		return false
	}
}

func (s *Subscriber) IncrementDrops() {
	s.drops.Add(1)
}

func (s *Subscriber) Drops() int64 {
	return s.drops.Load()
}

func buildDropMarker(sessionID string) []byte {
	marker := Chunk{
		SessionID: sessionID,
		Sequence:  0,
		Content:   "",
		Type:      "dropped",
		Timestamp: time.Now().UnixMilli(),
		Dropped:   1,
	}
	data, _ := json.Marshal(marker)
	return data
}
