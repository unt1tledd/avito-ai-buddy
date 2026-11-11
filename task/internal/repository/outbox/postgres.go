package outbox

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"avito-ai-buddy/task/internal/entity"
	"avito-ai-buddy/task/internal/port"
	"avito-ai-buddy/task/internal/repository/task/sqlc"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) WithTx(ctx context.Context, fn func(port.OutboxQueue) error) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		return fn(transaction{Queries: sqlc.New(tx)})
	})
}

type transaction struct {
	*sqlc.Queries
}

func (tx transaction) GetMessages(ctx context.Context, batchSize int32) ([]entity.OutboxMessage, error) {
	rows, err := tx.Queries.GetMessages(ctx, batchSize)
	if err != nil {
		return nil, err
	}

	messages := make([]entity.OutboxMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, entity.OutboxMessage{
			TaskID: row.TaskID, Payload: row.Payload, Attempts: row.Attempts,
		})
	}
	return messages, nil
}

func (tx transaction) MarkAsRetryable(ctx context.Context, ids []int64, retryDelay time.Duration) error {
	return tx.Queries.MarkAsRetryable(ctx, sqlc.MarkAsRetryableParams{
		TaskIds:    ids,
		RetryDelay: pgtype.Interval{Microseconds: retryDelay.Microseconds(), Valid: true},
	})
}
