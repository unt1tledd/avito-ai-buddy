package outbox

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"avito-ai-buddy/pkg/transactor"
	"avito-ai-buddy/task/internal/entity"
	"avito-ai-buddy/task/internal/repository/task/sqlc"
)

type PostgresRepository struct{}

func (PostgresRepository) GetMessages(ctx context.Context, batchSize int32) ([]entity.OutboxMessage, error) {
	queries, err := txQueries(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := queries.GetMessages(ctx, batchSize)
	if err != nil {
		return nil, err
	}

	messages := make([]entity.OutboxMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, entity.OutboxMessage{
			TaskID: row.TaskID, Payload: row.Payload, Attempts: row.Attempts, Failed: row.Failed,
		})
	}
	return messages, nil
}

func (PostgresRepository) MarkAsProcessed(ctx context.Context, ids []int64) error {
	queries, err := txQueries(ctx)
	if err != nil {
		return err
	}
	return queries.MarkAsProcessed(ctx, ids)
}

func (PostgresRepository) MarkAsRetryable(ctx context.Context, ids []int64, retryDelay time.Duration) error {
	queries, err := txQueries(ctx)
	if err != nil {
		return err
	}
	return queries.MarkAsRetryable(ctx, sqlc.MarkAsRetryableParams{
		TaskIds:    ids,
		RetryDelay: pgtype.Interval{Microseconds: retryDelay.Microseconds(), Valid: true},
	})
}

func (PostgresRepository) MarkAsFailed(ctx context.Context, ids []int64, retryDelay time.Duration) error {
	queries, err := txQueries(ctx)
	if err != nil {
		return err
	}
	return queries.MarkAsFailed(ctx, sqlc.MarkAsFailedParams{
		TaskIds:    ids,
		RetryDelay: pgtype.Interval{Microseconds: retryDelay.Microseconds(), Valid: true},
	})
}

func txQueries(ctx context.Context) (*sqlc.Queries, error) {
	tx, err := transactor.ExtractTx(ctx)
	if err != nil {
		return nil, err
	}
	return sqlc.New(tx), nil
}
