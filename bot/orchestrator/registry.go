package orchestrator

import "sync"

type SessionRegistry struct {
	mu         sync.RWMutex
	containers map[string]string
}

func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		containers: make(map[string]string),
	}
}

func (sr *SessionRegistry) Register(sessionID, containerID string) {
	sr.mu.Lock()
	sr.containers[sessionID] = containerID
	sr.mu.Unlock()
}

func (sr *SessionRegistry) Deregister(sessionID string) {
	sr.mu.Lock()
	delete(sr.containers, sessionID)
	sr.mu.Unlock()
}

func (sr *SessionRegistry) Get(sessionID string) (string, bool) {
	sr.mu.RLock()
	containerID, ok := sr.containers[sessionID]
	sr.mu.RUnlock()
	return containerID, ok
}

func (sr *SessionRegistry) ActiveSessionIDs() []string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	ids := make([]string, 0, len(sr.containers))
	for id := range sr.containers {
		ids = append(ids, id)
	}
	return ids
}
