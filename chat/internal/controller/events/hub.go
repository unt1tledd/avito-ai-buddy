package events

import "sync"

type StatusUpdate struct {
	TaskID            string `json:"task_id"`
	ExternalRequestID string `json:"external_request_id,omitempty"`
	Status            string `json:"status"`
}

type Hub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan StatusUpdate]struct{}
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[string]map[chan StatusUpdate]struct{})}
}

func (h *Hub) Subscribe(userID string) (<-chan StatusUpdate, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	updates := make(chan StatusUpdate, 16)
	if h.subscribers[userID] == nil {
		h.subscribers[userID] = make(map[chan StatusUpdate]struct{})
	}
	h.subscribers[userID][updates] = struct{}{}

	return updates, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.remove(userID, updates)
	}
}

func (h *Hub) Publish(userID string, update StatusUpdate) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for updates := range h.subscribers[userID] {
		select {
		case updates <- update:
		default:
			h.remove(userID, updates)
		}
	}
}

// remove is called with mu held.
func (h *Hub) remove(userID string, updates chan StatusUpdate) {
	if _, exists := h.subscribers[userID][updates]; !exists {
		return
	}
	delete(h.subscribers[userID], updates)
	close(updates)
	if len(h.subscribers[userID]) == 0 {
		delete(h.subscribers, userID)
	}
}
