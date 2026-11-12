package port

import (
	"context"
	"time"

	"avito-ai-buddy/task/internal/entity"
)

// OutboxQueue is used inside a transaction that holds the selected rows locked.
type OutboxQueue interface {
	GetMessages(context.Context, int32) ([]entity.OutboxMessage, error)
	MarkAsProcessed(context.Context, []int64) error
	MarkAsRetryable(context.Context, []int64, time.Duration) error
	MarkAsFailed(context.Context, []int64, time.Duration) error
}
