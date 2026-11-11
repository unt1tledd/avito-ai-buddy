// Package inbox makes delivery of incoming events idempotent: the event key
// is recorded in one transaction with the delivery effect, so Kafka
// redeliveries are skipped instead of processed twice.
package inbox

import (
	"context"
	"encoding/json"
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
	}

	// Inbox decorates a notifier with idempotent delivery.
	Inbox struct {
		queue Queue
		tx    port.Transactor
		next  port.TaskEventsNotifier
	}
)

func New(queue Queue, tx port.Transactor, next port.TaskEventsNotifier) *Inbox {
	return &Inbox{queue: queue, tx: tx, next: next}
}

// PublishTaskStatus marks the event as processed and delivers it in one
// transaction. If the delivery fails, the transaction rolls back, so the
// marker is not stored and the consumer can retry the delivery later.
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

	return i.tx.WithTx(ctx, func(ctx context.Context) error {
		inserted, err := i.queue.TryInsert(ctx, message)
		if err != nil {
			return fmt.Errorf("mark event as processed: %w", err)
		}
		if !inserted {
			// Kafka redelivery of an already processed event.
			return nil
		}
		return i.next.PublishTaskStatus(ctx, event)
	})
}

func eventKey(event port.TaskEvent) string {
	if event.ExternalRequestID != "" {
		return event.ExternalRequestID
	}
	return event.TaskID + "|" + event.Status
}
