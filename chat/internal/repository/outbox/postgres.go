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

// PostgresRepository stores and dispatches the outbox messages in
// PostgreSQL.
type PostgresRepository struct {
	queries sqlc.Querier
}

// NewPostgresRepository builds the repository over a pgx pool or transaction.
func NewPostgresRepository(db sqlc.DBTX) *PostgresRepository {
	return &PostgresRepository{queries: sqlc.New(db)}
}

// getQueries prefers the transaction from the context when present, so all
// statements of a usecase run atomically.
func (r *PostgresRepository) getQueries(ctx context.Context) sqlc.Querier {
	if tx, err := transactor.ExtractTx(ctx); err == nil {
		return sqlc.New(tx)
	}
	return r.queries
}

// SendOutboxMessage stores a command payload. Re-confirmation updates its
// dispatch ID after ResetMessage, in the same transaction under the action lock.
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

// GetMessages claims a batch of messages for dispatch, including ones stuck
// in progress longer than the configured TTL.
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

// MarkAsProcessed stores the successful delivery of the messages.
func (r *PostgresRepository) MarkAsProcessed(ctx context.Context, keys []string) error {
	return r.getQueries(ctx).MarkOutboxMessagesAsProcessed(ctx, keys)
}

// MarkAsRetryable returns the messages to CREATED and bumps the attempt
// counter after a failed dispatch.
func (r *PostgresRepository) MarkAsRetryable(ctx context.Context, keys []string) error {
	return r.getQueries(ctx).MarkOutboxMessagesAsRetryable(ctx, keys)
}

func (r *PostgresRepository) MarkAsFailed(ctx context.Context, keys []string) error {
	return r.getQueries(ctx).MarkOutboxMessagesAsFailed(ctx, keys)
}

// GetMessageStatus returns the dispatch status and attempt count of a single
// outbox message by its idempotency key.
func (r *PostgresRepository) GetMessageStatus(ctx context.Context, idempotencyKey string) (string, int32, error) {
	row, err := r.getQueries(ctx).GetOutboxMessage(ctx, idempotencyKey)
	if err != nil {
		return "", 0, err
	}
	return string(row.Status), row.Attempts, nil
}

// ResetMessage returns a failed delivery or failed Task processing attempt to
// CREATED. The caller replaces the payload in the same transaction.
func (r *PostgresRepository) ResetMessage(ctx context.Context, idempotencyKey string) error {
	return r.getQueries(ctx).ResetOutboxMessage(ctx, idempotencyKey)
}
