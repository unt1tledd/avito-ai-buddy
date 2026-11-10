package action

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

import (
	"context"
	"errors"

	"avito-ai-buddy/chat/internal/entity"
	chaterrors "avito-ai-buddy/chat/internal/errors"
	sqlc "avito-ai-buddy/chat/internal/repository/action/sqlc"
	"avito-ai-buddy/pkg/transactor"

	"github.com/jackc/pgx/v5"
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

func (r *PostgresRepository) Create(ctx context.Context, action entity.Action) (int64, error) {
	return r.getQueries(ctx).Create(ctx, sqlc.CreateParams{
		MessageID:     action.MessageID,
		ResourceID:    action.ResourceID,
		ResourceLabel: action.ResourceLabel,
		AccessLevel:   action.AccessLevel,
	})
}

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
