package orchestrator

import (
	"sync"
	"time"
)

type ContainerTimer struct {
	mu          sync.Mutex
	timer       *time.Timer
	remainingMs int64
	startedAt   int64
	done        chan struct{}
	cancelled   bool
}

func NewContainerTimer(totalMs int64) *ContainerTimer {
	ct := &ContainerTimer{
		remainingMs: totalMs,
		done:        make(chan struct{}),
	}
	ct.start()
	return ct
}

func (ct *ContainerTimer) start() {
	ct.startedAt = time.Now().UnixMilli()
	ct.timer = time.AfterFunc(time.Duration(ct.remainingMs)*time.Millisecond, func() {
		ct.mu.Lock()
		defer ct.mu.Unlock()
		if !ct.cancelled {
			close(ct.done)
		}
	})
}

func (ct *ContainerTimer) Pause() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.cancelled || ct.timer == nil {
		return
	}

	elapsed := time.Now().UnixMilli() - ct.startedAt
	ct.remainingMs -= elapsed
	if ct.remainingMs < 0 {
		ct.remainingMs = 0
	}

	ct.timer.Stop()
	ct.timer = nil
}

func (ct *ContainerTimer) Resume() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.cancelled || ct.timer != nil {
		return
	}

	ct.start()
}

func (ct *ContainerTimer) Cancel() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.cancelled = true
	if ct.timer != nil {
		ct.timer.Stop()
		ct.timer = nil
	}
}

func (ct *ContainerTimer) Done() <-chan struct{} {
	return ct.done
}
