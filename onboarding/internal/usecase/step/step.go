package step

import (
	"context"
	"errors"
	"strings"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
)

//go:generate mockgen -source=step.go -destination=mocks/step_mocks.go -package=mocks

type planRepository interface {
	GetPlanByUserID(ctx context.Context, userID int64) (entity.Plan, error)
}

type stepRepository interface {
	GetCurrentStep(ctx context.Context, planID int64) (*entity.Step, error)
	MarkStepCompleted(ctx context.Context, userID int64, stepID string) error
	ReopenStep(ctx context.Context, userID int64, stepID string) error
	UpdateStepTemplate(ctx context.Context, template entity.StepTemplate) error
}

func (s *StepService) UpdateStepTemplate(ctx context.Context, template entity.StepTemplate) error {
	return s.steps.UpdateStepTemplate(ctx, template)
}

type StepService struct {
	plans planRepository
	steps stepRepository
}

func NewStepService(plans planRepository, steps stepRepository) *StepService {
	return &StepService{plans: plans, steps: steps}
}

func (s *StepService) GetCurrentStep(ctx context.Context, userID int64) (*entity.Step, error) {
	if userID <= 0 {
		return nil, onboardingerrors.ErrInvalidUserID
	}
	plan, err := s.plans.GetPlanByUserID(ctx, userID)
	if errors.Is(err, onboardingerrors.ErrPlanNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.steps.GetCurrentStep(ctx, plan.ID)
}

func (s *StepService) MarkStepCompleted(ctx context.Context, userID int64, stepID string) error {
	if err := validateStepTarget(userID, stepID); err != nil {
		return err
	}
	return s.steps.MarkStepCompleted(ctx, userID, stepID)
}

func (s *StepService) ReopenStep(ctx context.Context, userID int64, stepID string) error {
	if err := validateStepTarget(userID, stepID); err != nil {
		return err
	}
	return s.steps.ReopenStep(ctx, userID, stepID)
}

func validateStepTarget(userID int64, stepID string) error {
	if userID <= 0 {
		return onboardingerrors.ErrInvalidUserID
	}
	if strings.TrimSpace(stepID) == "" {
		return onboardingerrors.ErrInvalidStepID
	}
	return nil
}
