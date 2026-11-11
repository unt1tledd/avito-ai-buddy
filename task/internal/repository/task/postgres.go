package task

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"avito-ai-buddy/task/internal/entity"
	taskerrors "avito-ai-buddy/task/internal/errors"
	"avito-ai-buddy/task/internal/repository/task/sqlc"
)

type PostgresRepository struct {
	db      *pgxpool.Pool
	queries *sqlc.Queries
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db, queries: sqlc.New(db)}
}

func (r *PostgresRepository) GetByProposal(ctx context.Context, proposalID int64) (entity.AccessRequest, error) {
	row, err := r.queries.GetByProposal(ctx, proposalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AccessRequest{}, taskerrors.ErrNotFound
	}
	return toEntity(row), err
}

func (r *PostgresRepository) Create(ctx context.Context, request entity.CreateRequest, teamID int64) (entity.AccessRequest, error) {
	row, err := r.queries.CreateAccessRequest(ctx, sqlc.CreateAccessRequestParams{
		ProposalID: request.ProposalID, UserID: request.UserID, TeamID: teamID,
		ResourceID: request.ResourceID, AccessLevel: request.AccessLevel,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		return toEntity(row), err
	}

	existing, err := r.GetByProposal(ctx, request.ProposalID)
	if err != nil {
		return entity.AccessRequest{}, err
	}
	if existing.UserID != request.UserID || existing.ResourceID != request.ResourceID || existing.AccessLevel != request.AccessLevel {
		return entity.AccessRequest{}, taskerrors.ErrProposalConflict
	}
	return existing, nil
}

func (r *PostgresRepository) ListPending(ctx context.Context, teamID, afterID int64, limit int32) ([]entity.AccessRequest, error) {
	rows, err := r.queries.ListPending(ctx, sqlc.ListPendingParams{TeamID: teamID, AfterID: afterID, PageSize: limit})
	if err != nil {
		return nil, err
	}
	requests := make([]entity.AccessRequest, 0, len(rows))
	for _, row := range rows {
		requests = append(requests, toEntity(row))
	}
	return requests, nil
}

func (r *PostgresRepository) Decide(ctx context.Context, decision entity.Decision) (entity.AccessRequest, error) {
	var result entity.AccessRequest
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		queries := r.queries.WithTx(tx)
		row, err := queries.Decide(ctx, sqlc.DecideParams{
			ID: decision.TaskID, TeamID: decision.TeamID, Status: string(decision.Status),
			DecidedByUserID: &decision.DecidedByUserID, DecisionComment: decision.Comment,
			ExternalRequestID: decision.ExternalRequestID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			row, err := queries.GetByTeam(ctx, sqlc.GetByTeamParams{ID: decision.TaskID, TeamID: decision.TeamID})
			if errors.Is(err, pgx.ErrNoRows) {
				return taskerrors.ErrNotFound
			}
			if err != nil {
				return err
			}
			if row.Status != string(decision.Status) || row.DecidedByUserID == nil ||
				*row.DecidedByUserID != decision.DecidedByUserID || row.DecisionComment != decision.Comment ||
				row.ExternalRequestID != decision.ExternalRequestID {
				return taskerrors.ErrAlreadyDecided
			}
			result = toEntity(row)
			return nil
		}
		if err != nil {
			return err
		}

		result = toEntity(row)
		payload, err := json.Marshal(entity.StatusEvent{
			UserID: result.UserID, TaskID: result.ID, Status: result.Status,
			ExternalRequestID: result.ExternalRequestID,
		})
		if err != nil {
			return err
		}
		return queries.InsertEvent(ctx, sqlc.InsertEventParams{TaskID: result.ID, Payload: payload})
	})
	if err != nil {
		var dbError *pgconn.PgError
		if errors.As(err, &dbError) && dbError.Code == "23505" && dbError.ConstraintName == "access_request_external_id_idx" {
			return entity.AccessRequest{}, taskerrors.ErrExternalConflict
		}
		return entity.AccessRequest{}, err
	}
	return result, nil
}

func toEntity(row sqlc.TaskAccessRequest) entity.AccessRequest {
	result := entity.AccessRequest{
		ID: row.ID, ProposalID: row.ProposalID, UserID: row.UserID, TeamID: row.TeamID,
		ResourceID: row.ResourceID, AccessLevel: row.AccessLevel, Status: entity.Status(row.Status),
		CreatedAt: row.CreatedAt.Time, DecidedByUserID: row.DecidedByUserID,
		DecisionComment: row.DecisionComment, ExternalRequestID: row.ExternalRequestID,
	}
	if row.DecidedAt.Valid {
		result.DecidedAt = &row.DecidedAt.Time
	}
	return result
}
