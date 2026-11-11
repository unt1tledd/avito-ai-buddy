package outbox

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

import (
	"context"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	sqlc "avito-ai-buddy/chat/internal/repository/outbox/sqlc"
	"avito-ai-buddy/pkg/transactor"

	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresRepository struct {
	queries sqlc.Querier
}

func NewPostgresRepository(db sqlc.DBTX) *PostgresRepository {
	return &PostgresRepository{queries: sqlc.New(db)}
}

func (r *PostgresRepository) getQueries(ctx context.Context) sqlc.Querier {
	if tx, err := transactor.ExtractTx(ctx); err == nil {
		return sqlc.New(tx)
	}
	return r.queries
}

func (r *PostgresRepository) SendOutboxMessage(ctx context.Context, message entity.OutboxMessage) error {
	return r.getQueries(ctx).SendOutboxMessage(ctx, sqlc.SendOutboxMessageParams{
		IdempotencyKey: message.IdempotencyKey,
		Data:           message.Data,
		Kind:           message.Kind,
	})
}

func (r *PostgresRepository) GetMessages(ctx context.Context, batchSize int32, inProgressTTL time.Duration) ([]entity.OutboxMessage, error) {
	rows, err := r.getQueries(ctx).GetOutboxMessages(ctx, sqlc.GetOutboxMessagesParams{
		BatchSize:     batchSize,
		InProgressTtl: pgtype.Interval{Microseconds: inProgressTTL.Microseconds(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	messages := make([]entity.OutboxMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, entity.OutboxMessage{
			IdempotencyKey: row.IdempotencyKey,
			Data:           row.Data,
			Kind:           row.Kind,
		})
	}
	return messages, nil
}

func (r *PostgresRepository) MarkAsProcessed(ctx context.Context, keys []string) error {
	return r.getQueries(ctx).MarkOutboxMessagesAsProcessed(ctx, keys)
}

func (r *PostgresRepository) MarkAsRetryable(ctx context.Context, keys []string) error {
	return r.getQueries(ctx).MarkOutboxMessagesAsRetryable(ctx, keys)
}
