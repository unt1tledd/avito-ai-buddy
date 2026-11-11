package plan

import (
	"context"
	"errors"
	"strings"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
)

//go:generate mockgen -source=plan.go -destination=mocks/plan_mocks.go -package=mocks

type planRepository interface {
	AssignPlan(ctx context.Context, userID int64, track string) (entity.Plan, error)
	GetPlanByUserID(ctx context.Context, userID int64) (entity.Plan, error)
}

type stepRepository interface {
	GetPlanSteps(ctx context.Context, planID int64) ([]entity.Step, error)
}

type userClient interface {
	GetTrack(ctx context.Context, userID int64) (string, error)
}

type PlanService struct {
	plans      planRepository
	steps      stepRepository
	userClient userClient
}

func NewPlanService(plans planRepository, steps stepRepository, userClient userClient) *PlanService {
	return &PlanService{plans: plans, steps: steps, userClient: userClient}
}

func (s *PlanService) AssignPlan(ctx context.Context, userID int64, track string) (entity.Plan, error) {
	if userID <= 0 {
		return entity.Plan{}, onboardingerrors.ErrInvalidUserID
	}
	if strings.TrimSpace(track) == "" {
		existing, err := s.plans.GetPlanByUserID(ctx, userID)
		if err == nil {
			existing.Steps, err = s.steps.GetPlanSteps(ctx, existing.ID)
			return existing, err
		}
		if !errors.Is(err, onboardingerrors.ErrPlanNotFound) {
			return entity.Plan{}, err
		}
		userTrack, err := s.userClient.GetTrack(ctx, userID)
		if err != nil {
			return entity.Plan{}, err
		}
		track = userTrack
	}
	track = strings.ToLower(strings.TrimSpace(track))

	plan, err := s.plans.AssignPlan(ctx, userID, track)
	if err != nil {
		return entity.Plan{}, err
	}
	plan.Steps, err = s.steps.GetPlanSteps(ctx, plan.ID)
	return plan, err
}

func (s *PlanService) GetPlan(ctx context.Context, userID int64) (entity.Plan, error) {
	if userID <= 0 {
		return entity.Plan{}, onboardingerrors.ErrInvalidUserID
	}
	plan, err := s.plans.GetPlanByUserID(ctx, userID)
	if err != nil {
		return entity.Plan{}, err
	}
	plan.Steps, err = s.steps.GetPlanSteps(ctx, plan.ID)
	return plan, err
}
