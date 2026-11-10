package plan

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	sqlc "avito-ai-buddy/onboarding/internal/repository/plan/sqlc"
	"avito-ai-buddy/pkg/transactor"
)

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

type planTransactor interface {
	WithTx(context.Context, func(context.Context) error) error
}

type PlanRepository struct {
	queries sqlc.Querier
	tx      planTransactor
}

func NewPlanRepository(queries sqlc.Querier, tx planTransactor) *PlanRepository {
	return &PlanRepository{queries: queries, tx: tx}
}

func (r *PlanRepository) AssignPlan(ctx context.Context, userID int64, track string) (entity.Plan, error) {
	var plan entity.Plan
	err := r.tx.WithTx(ctx, func(txCtx context.Context) error {
		tx, err := transactor.ExtractTx(txCtx)
		if err != nil {
			return err
		}
		queries := sqlc.New(tx)
		if _, err := queries.LockTrackTemplate(txCtx, track); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return onboardingerrors.ErrUnsupportedTrack
			}
			return err
		}

		row, err := queries.AssignPlan(txCtx, sqlc.AssignPlanParams{UserID: userID, Track: track})
		if errors.Is(err, pgx.ErrNoRows) {
			return onboardingerrors.ErrPlanTrackConflict
		}
		if err != nil {
			return err
		}
		plan = entity.Plan{ID: row.ID, UserID: row.UserID, Track: row.Track}
		return nil
	})
	if err != nil {
		return entity.Plan{}, err
	}
	return plan, nil
}

func (r *PlanRepository) GetPlanByUserID(ctx context.Context, userID int64) (entity.Plan, error) {
	row, err := r.queries.GetPlanByUserID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Plan{}, onboardingerrors.ErrPlanNotFound
	}
	if err != nil {
		return entity.Plan{}, err
	}
	return entity.Plan{ID: row.ID, UserID: row.UserID, Track: row.Track}, nil
}
