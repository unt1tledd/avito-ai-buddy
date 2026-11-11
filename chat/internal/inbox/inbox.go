// Package inbox records incoming events and their effects in one transaction.
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

	taskStatusHandler interface {
		SaveTaskStatus(ctx context.Context, event port.TaskEvent) (*entity.Message, error)
		PublishSavedTaskStatus(ctx context.Context, event port.TaskEvent, message entity.Message)
	}

	// Inbox processes task status events idempotently.
	Inbox struct {
		queue      Queue
		taskStatus taskStatusHandler
		tx         port.Transactor
	}
)

func New(queue Queue, taskStatus taskStatusHandler, tx port.Transactor) *Inbox {
	return &Inbox{queue: queue, taskStatus: taskStatus, tx: tx}
}

// PublishTaskStatus saves the inbox marker and the chat message atomically.
// It pushes a newly saved message to subscribers after commit.
func (i *Inbox) PublishTaskStatus(ctx context.Context, event port.TaskEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode inbox payload: %w", err)
	}
	message := entity.InboxMessage{
		IdempotencyKey: event.IdempotencyKey(),
		Data:           payload,
		Kind:           inboxKindTaskStatusChange,
	}

	var saved *entity.Message
	err = i.tx.WithTx(ctx, func(txCtx context.Context) error {
		inserted, err := i.queue.TryInsert(txCtx, message)
		if err != nil {
			return fmt.Errorf("mark event as processed: %w", err)
		}
		if !inserted {
			return nil
		}

		saved, err = i.taskStatus.SaveTaskStatus(txCtx, event)
		return err
	})
	if err != nil {
		return err
	}
	if saved != nil {
		i.taskStatus.PublishSavedTaskStatus(ctx, event, *saved)
	}
	return nil
}
