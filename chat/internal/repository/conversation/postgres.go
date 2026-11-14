package conversation

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

import (
	"context"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"strconv"

	"avito-ai-buddy/chat/internal/entity"
	sqlc "avito-ai-buddy/chat/internal/repository/conversation/sqlc"
	"avito-ai-buddy/pkg/transactor"
)

// PostgresRepository stores intern conversations in PostgreSQL.
type PostgresRepository struct {
	queries sqlc.Querier
	db      sqlc.DBTX
}

// NewPostgresRepository builds the repository over a pgx pool or transaction.
func NewPostgresRepository(db sqlc.DBTX) *PostgresRepository {
	return &PostgresRepository{queries: sqlc.New(db), db: db}
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

	db := r.db
	if tx, err := transactor.ExtractTx(ctx); err == nil {
		db = tx
	}
	selected := metadata.ValueFromIncomingContext(ctx, "x-conversation-id")
	result := &entity.Conversation{}
	var row pgx.Row
	if len(selected) > 0 {
		id, err := strconv.ParseInt(selected[0], 10, 64)
		if err != nil || id <= 0 {
			return nil, status.Error(codes.InvalidArgument, "invalid conversation ID")
		}
		row = db.QueryRow(ctx, `SELECT id,user_id,created_at FROM chat.conversation WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, id, userID)
	} else {
		row, err := r.getQueries(ctx).GetOrCreate(ctx, userID)
		if err != nil {
			return nil, err
		}
		return &entity.Conversation{ID: row.ID, UserID: row.UserID, CreatedAt: row.CreatedAt.Time}, nil
	}
	if err := row.Scan(&result.ID, &result.UserID, &result.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, status.Error(codes.NotFound, "conversation not found")
		}
		return nil, err
	}
	return result, nil
}

// GetForTask routes asynchronous task updates to the proposal's own conversation.
func (r *PostgresRepository) GetForTask(ctx context.Context, userID int64, taskID string) (*entity.Conversation, error) {
	db := r.db
	if tx, err := transactor.ExtractTx(ctx); err == nil {
		db = tx
	}
	result := &entity.Conversation{}
	err := db.QueryRow(ctx, `SELECT c.id,c.user_id,c.created_at FROM chat.conversation c JOIN chat.message m ON m.conversation_id=c.id JOIN chat.action a ON a.message_id=m.id WHERE c.user_id=$1 AND a.task_id=$2 AND c.deleted_at IS NULL LIMIT 1`, userID, taskID).Scan(&result.ID, &result.UserID, &result.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}
