// Package inbox makes delivery of incoming events idempotent: the event key
// is recorded before delivery, so Kafka redeliveries are skipped. When the
// delivery fails, the marker is removed and the event is processed again on
// the next redelivery.
package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"avito-ai-buddy/chat/internal/entity"
	"avito-ai-buddy/chat/internal/port"
)

const inboxKindTaskStatusChange int32 = 1

//go:generate mockgen -source=inbox.go -destination=mocks/inbox_mocks.go -package=mocks

type (
	// Queue stores the processed event markers.
	Queue interface {
		TryInsert(ctx context.Context, message entity.InboxMessage) (bool, error)
		Delete(ctx context.Context, idempotencyKey string) error
	}

	// Inbox decorates a notifier with idempotent delivery.
	Inbox struct {
		queue Queue
		next  port.TaskEventsNotifier
	}
)

func New(queue Queue, next port.TaskEventsNotifier) *Inbox {
	return &Inbox{queue: queue, next: next}
}

// PublishTaskStatus skips already processed events and delivers new ones. If
// the delivery fails, the marker is rolled back so the next redelivery can
// process the event again. The rollback is best effort: a crash between the
// delivery and the rollback may lose the event, which is acceptable for
// ephemeral WebSocket notifications.
func (i *Inbox) PublishTaskStatus(ctx context.Context, event port.TaskEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode inbox payload: %w", err)
	}
	message := entity.InboxMessage{
		IdempotencyKey: eventKey(event),
		Data:           payload,
		Kind:           inboxKindTaskStatusChange,
	}

	inserted, err := i.queue.TryInsert(ctx, message)
	if err != nil {
		return fmt.Errorf("mark event as processed: %w", err)
	}
	if !inserted {
		// Kafka redelivery of an already processed event.
		return nil
	}

	if err := i.next.PublishTaskStatus(ctx, event); err != nil {
		if delErr := i.queue.Delete(ctx, message.IdempotencyKey); delErr != nil {
			return errors.Join(err, fmt.Errorf("roll back processed marker: %w", delErr))
		}
		return err
	}
	return nil
}

func eventKey(event port.TaskEvent) string {
	if event.ExternalRequestID != "" {
		return event.ExternalRequestID
	}
	return event.TaskID + "|" + event.Status
}
