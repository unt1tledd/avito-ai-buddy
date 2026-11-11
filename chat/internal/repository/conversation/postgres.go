package conversation

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

import (
	"context"

	"avito-ai-buddy/chat/internal/entity"
	sqlc "avito-ai-buddy/chat/internal/repository/conversation/sqlc"
	"avito-ai-buddy/pkg/transactor"
)

// PostgresRepository stores intern conversations in PostgreSQL.
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

// GetOrCreate returns the intern's conversation, creating it on first use.
func (r *PostgresRepository) GetOrCreate(ctx context.Context, userID int64) (*entity.Conversation, error) {
	row, err := r.getQueries(ctx).GetOrCreate(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &entity.Conversation{
		ID:        row.ID,
		UserID:    row.UserID,
		CreatedAt: row.CreatedAt.Time,
	}, nil
}
