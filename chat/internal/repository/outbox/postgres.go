package outbox

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

import (
	"context"
	"errors"
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

// FetchOptions carries the dispatch parameters for GetMessages through the
// context: the worker injects them from config on every poll.
type FetchOptions struct {
	BatchSize     int32
	InProgressTTL time.Duration
}

type fetchOptionsKey struct{}

// WithFetchOptions returns a context carrying the outbox fetch parameters.
func WithFetchOptions(ctx context.Context, options FetchOptions) context.Context {
	return context.WithValue(ctx, fetchOptionsKey{}, options)
}

// FetchOptionsFromContext extracts the outbox fetch parameters previously
// stored by WithFetchOptions.
func FetchOptionsFromContext(ctx context.Context) (FetchOptions, bool) {
	options, ok := ctx.Value(fetchOptionsKey{}).(FetchOptions)
	return options, ok
}

func (r *PostgresRepository) GetMessages(ctx context.Context) ([]entity.OutboxMessage, error) {
	options, ok := FetchOptionsFromContext(ctx)
	if !ok {
		return nil, errors.New("outbox fetch options are not set in context")
	}
	rows, err := r.getQueries(ctx).GetOutboxMessages(ctx, sqlc.GetOutboxMessagesParams{
		BatchSize:     options.BatchSize,
		InProgressTtl: pgtype.Interval{Microseconds: options.InProgressTTL.Microseconds(), Valid: true},
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
			Attempts:       row.Attempts,
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

func (r *PostgresRepository) MarkAsFailed(ctx context.Context, keys []string) error {
	return r.getQueries(ctx).MarkOutboxMessagesAsFailed(ctx, keys)
}
