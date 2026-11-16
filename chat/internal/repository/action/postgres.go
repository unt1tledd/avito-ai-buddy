package action

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

import (
	"context"
	"errors"
	"fmt"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	"avito-ai-buddy/chat/internal/port"
	sqlc "avito-ai-buddy/chat/internal/repository/action/sqlc"
	"avito-ai-buddy/pkg/transactor"

	"github.com/jackc/pgx/v5"
)

// PostgresRepository stores access proposals and their request state in
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

// Create inserts an access proposal attached to its assistant message and
// returns the generated ID.
func (r *PostgresRepository) Create(ctx context.Context, action entity.Action) (int64, error) {
	return r.getQueries(ctx).Create(ctx, sqlc.CreateParams{
		MessageID:     action.MessageID,
		ResourceID:    action.ResourceID,
		ResourceLabel: action.ResourceLabel,
		AccessLevel:   action.AccessLevel,
	})
}

// GetByID returns the proposal owned by the user, or nil when the proposal
// does not exist or belongs to another intern.
func (r *PostgresRepository) GetByID(ctx context.Context, userID int64, proposalID int64) (*entity.Action, error) {
	row, err := r.getQueries(ctx).GetByID(ctx, sqlc.GetByIDParams{ID: proposalID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	action := &entity.Action{
		ID:            row.ID,
		MessageID:     row.MessageID,
		ResourceID:    row.ResourceID,
		ResourceLabel: row.ResourceLabel,
		AccessLevel:   row.AccessLevel,
		CreatedAt:     row.CreatedAt.Time,
	}
	if row.Status != "" {
		action.AccessRequest = &entity.AccessRequest{
			TaskID:            row.TaskID,
			ExternalRequestID: row.ExternalRequestID,
			Status:            row.Status,
		}
	}
	return action, nil
}

// SaveAccessRequest records the dispatch state of a confirmed proposal. It
// fails when the proposal does not exist.
func (r *PostgresRepository) SaveAccessRequest(ctx context.Context, proposalID int64, request entity.AccessRequest) error {
	affected, err := r.getQueries(ctx).SaveAccessRequest(ctx, sqlc.SaveAccessRequestParams{
		ID:                proposalID,
		TaskID:            request.TaskID,
		ExternalRequestID: request.ExternalRequestID,
		Status:            request.Status,
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return chaterrors.ErrProposalNotFound
	}
	return nil
}

// ApplyTaskStatus returns the ID of the visible conversation to notify.
// A missing proposal or deleted conversation has no notification target.
func (r *PostgresRepository) ApplyTaskStatus(ctx context.Context, userID int64, event port.TaskEvent) (int64, error) {
	queries := r.getQueries(ctx)
	if event.Status == entity.TaskPending || event.Status == entity.TaskFailed {
		tx, err := transactor.ExtractTx(ctx)
		if err != nil {
			return 0, fmt.Errorf("lifecycle status requires a transaction: %w", err)
		}
		queries = sqlc.New(tx)
		// Serialize with ConfirmProposal before reading the dispatch ID. A separate
		// statement gets a fresh read-committed snapshot after a concurrent retry,
		// instead of comparing with an old outbox snapshot while waiting on UPDATE.
		if _, err := queries.GetByID(ctx, sqlc.GetByIDParams{ID: event.ProposalID, UserID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, nil
			}
			return 0, err
		}
	}
	row, err := queries.ApplyTaskStatus(ctx, sqlc.ApplyTaskStatusParams{
		UserID: userID, ProposalID: event.ProposalID, TaskID: event.TaskID,
		ExternalRequestID: event.ExternalRequestID, Status: string(event.Status),
		DispatchID: event.DispatchID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if row.ConversationDeleted {
		return 0, nil
	}
	return row.ConversationID, nil
}
