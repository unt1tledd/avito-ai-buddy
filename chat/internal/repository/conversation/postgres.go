package conversation

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

import (
	"context"
	"errors"
	"strconv"

	"avito-ai-buddy/chat/internal/entity"
	sqlc "avito-ai-buddy/chat/internal/repository/conversation/sqlc"
	"avito-ai-buddy/pkg/transactor"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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

// GetOrCreate loads the selected conversation or opens the default one.
func (r *PostgresRepository) GetOrCreate(ctx context.Context, userID int64) (*entity.Conversation, error) {
	queries := r.getQueries(ctx)
	selected := metadata.ValueFromIncomingContext(ctx, "x-conversation-id")
	if len(selected) == 0 {
		row, err := queries.GetOrCreate(ctx, userID)
		if err != nil {
			return nil, err
		}
		return &entity.Conversation{ID: row.ID, UserID: row.UserID, CreatedAt: row.CreatedAt.Time}, nil
	}
	id, err := strconv.ParseInt(selected[0], 10, 64)
	if err != nil || id <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid conversation ID")
	}
	row, err := queries.GetByID(ctx, sqlc.GetByIDParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "conversation not found")
	}
	if err != nil {
		return nil, err
	}
	return &entity.Conversation{ID: row.ID, UserID: row.UserID, CreatedAt: row.CreatedAt.Time}, nil
}

func (r *PostgresRepository) List(ctx context.Context, userID int64) ([]entity.ConversationSummary, error) {
	rows, err := r.getQueries(ctx).List(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]entity.ConversationSummary, 0, len(rows))
	for _, row := range rows {
		result = append(result, entity.ConversationSummary{ID: row.ID, Title: row.Title, CreatedAt: row.CreatedAt.Time})
	}
	return result, nil
}

func (r *PostgresRepository) Create(ctx context.Context, userID int64) (entity.ConversationSummary, error) {
	row, err := r.getQueries(ctx).Create(ctx, userID)
	return entity.ConversationSummary{ID: row.ID, Title: row.Title, CreatedAt: row.CreatedAt.Time}, err
}

func (r *PostgresRepository) Delete(ctx context.Context, userID, id int64) (bool, error) {
	affected, err := r.getQueries(ctx).Delete(ctx, sqlc.DeleteParams{UserID: userID, ID: id})
	return affected > 0, err
}

func (r *PostgresRepository) DeleteAll(ctx context.Context, userID int64) error {
	return r.getQueries(ctx).DeleteAll(ctx, userID)
}
