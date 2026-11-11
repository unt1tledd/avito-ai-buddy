package inbox

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

import (
	"context"
	"errors"
	"time"

	"avito-ai-buddy/chat/internal/entity"
	sqlc "avito-ai-buddy/chat/internal/repository/inbox/sqlc"
	"avito-ai-buddy/pkg/transactor"

	"github.com/jackc/pgx/v5"
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

// TryInsert marks the event as seen. It returns true only when the idempotency
// key was inserted by this call, i.e. the event is delivered for the first time.
func (r *PostgresRepository) TryInsert(ctx context.Context, message entity.InboxMessage) (bool, error) {
	_, err := r.getQueries(ctx).TryInsertInboxMessage(ctx, sqlc.TryInsertInboxMessageParams{
		IdempotencyKey: message.IdempotencyKey,
		Kind:           message.Kind,
		Payload:        message.Data,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT DO NOTHING skipped the insert: the event was already processed.
		return false, nil
	}
	return err == nil, err
}

func (r *PostgresRepository) CleanupOlderThan(ctx context.Context, retention time.Duration) error {
	return r.getQueries(ctx).CleanupInboxMessages(ctx, pgtype.Interval{
		Microseconds: retention.Microseconds(),
		Valid:        true,
	})
}
