package step

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	sqlc "avito-ai-buddy/onboarding/internal/repository/step/sqlc"
)

//go:generate mockgen -source=sqlc/querier.go -destination=mocks/querier_mocks.go -package=mocks

type StepRepository struct {
	queries sqlc.Querier
}

func NewStepRepository(queries sqlc.Querier) *StepRepository {
	return &StepRepository{queries: queries}
}

func (r *StepRepository) GetPlanSteps(ctx context.Context, planID int64) ([]entity.Step, error) {
	rows, err := r.queries.GetPlanSteps(ctx, planID)
	if err != nil {
		return nil, err
	}
	steps := make([]entity.Step, 0, len(rows))
	for _, row := range rows {
		status := entity.StepPending
		if row.CompletedAt.Valid {
			status = entity.StepCompleted
		}
		steps = append(steps, entity.Step{
			ID: row.ID, Track: row.Track, Title: row.Title, Description: row.Description,
			Period: row.Period, ResourceID: row.ResourceID, Status: status,
		})
	}
	return steps, nil
}

func (r *StepRepository) GetCurrentStep(ctx context.Context, planID int64) (*entity.Step, error) {
	row, err := r.queries.GetCurrentStep(ctx, planID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entity.Step{
		ID: row.ID, Title: row.Title, Description: row.Description,
		Period: row.Period, ResourceID: row.ResourceID, Status: entity.StepPending,
	}, nil
}

func (r *StepRepository) MarkStepCompleted(ctx context.Context, userID int64, stepID string) error {
	_, err := r.queries.MarkStepCompleted(ctx, sqlc.MarkStepCompletedParams{UserID: userID, StepKey: stepID})
	if errors.Is(err, pgx.ErrNoRows) {
		return onboardingerrors.ErrStepNotFound
	}
	return err
}

func (r *StepRepository) ReopenStep(ctx context.Context, userID int64, stepID string) error {
	_, err := r.queries.ReopenStep(ctx, sqlc.ReopenStepParams{UserID: userID, StepKey: stepID})
	if errors.Is(err, pgx.ErrNoRows) {
		return onboardingerrors.ErrStepNotFound
	}
	return err
}

func (r *StepRepository) UpdateStepTemplate(ctx context.Context, template entity.StepTemplate) error {
	_, err := r.queries.UpdateStepTemplate(ctx, sqlc.UpdateStepTemplateParams{
		StepKey: template.Key, Title: template.Title, Description: template.Description,
		Period: template.Period, Position: template.Position, ResourceID: template.ResourceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return onboardingerrors.ErrStepTemplateNotFound
	}
	return err
}
