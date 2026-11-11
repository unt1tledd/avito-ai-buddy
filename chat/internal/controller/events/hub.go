package events

import (
	"context"
	"fmt"
	"sync"

	"avito-ai-buddy/chat/internal/port"
)

// Event is a server-pushed WebSocket notification delivered to subscribers.
type Event struct {
	Type string `json:"type"`
	StatusUpdate
}

// Status update event types delivered over the WebSocket channel.
const (
	EventTypeTaskStatusChanged   = "task.status_changed"
	EventTypeAccessRequestFailed = "access_request.failed"
)

type StatusUpdate struct {
	TaskID            string `json:"task_id"`
	ExternalRequestID string `json:"external_request_id,omitempty"`
	Status            string `json:"status"`
}

// Hub keeps the open WebSocket subscriptions per user and delivers events to
// them. It satisfies port.TaskEventsNotifier for the Kafka consumer and also
// reports failed outbox deliveries.
type Hub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[string]map[chan Event]struct{})}
}

// Subscribe registers a channel receiving the user's events. The returned
// unsubscribe function must be called when the connection closes.
func (h *Hub) Subscribe(userID string) (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	updates := make(chan Event, 16)
	if h.subscribers[userID] == nil {
		h.subscribers[userID] = make(map[chan Event]struct{})
	}
	h.subscribers[userID][updates] = struct{}{}

	return updates, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.remove(userID, updates)
	}
}

// PublishTaskStatus makes Hub satisfy port.TaskEventsNotifier: task status
// updates consumed from Kafka are routed to the owner's WebSocket subscribers.
func (h *Hub) PublishTaskStatus(_ context.Context, event port.TaskEvent) error {
	h.publish(event.UserID, Event{
		Type:         EventTypeTaskStatusChanged,
		StatusUpdate: StatusUpdate{TaskID: event.TaskID, ExternalRequestID: event.ExternalRequestID, Status: event.Status},
	})
	return nil
}

// PublishAccessRequestFailed tells the owner that their access request could
// not be delivered and is parked as FAILED in the outbox.
func (h *Hub) PublishAccessRequestFailed(_ context.Context, userID string, proposalID int64, idempotencyKey string) error {
	h.publish(userID, Event{
		Type:         EventTypeAccessRequestFailed,
		StatusUpdate: StatusUpdate{TaskID: fmt.Sprintf("%d", proposalID), ExternalRequestID: idempotencyKey, Status: "failed"},
	})
	return nil
}

func (h *Hub) publish(userID string, event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for updates := range h.subscribers[userID] {
		select {
		case updates <- event:
		default:
			h.remove(userID, updates)
		}
	}
}

// remove is called with mu held.
func (h *Hub) remove(userID string, updates chan Event) {
	if _, exists := h.subscribers[userID][updates]; !exists {
		return
	}
	delete(h.subscribers[userID], updates)
	close(updates)
	if len(h.subscribers[userID]) == 0 {
		delete(h.subscribers, userID)
	}
}
