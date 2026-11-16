package grpc

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"avito-ai-buddy/chat/internal/entity"
	planv1 "avito-ai-buddy/pkg/generated/plan/v1"
	"avito-ai-buddy/pkg/observability"
)

type planClient interface {
	GetPlanContext(context.Context, *planv1.GetPlanContextRequest, ...grpc.CallOption) (*planv1.GetPlanContextResponse, error)
}

// Client implements the onboarding plans port over the onboarding service
// gRPC API.
type Client struct {
	plans planClient
}

// NewClient builds a client on top of a ready plan service.
func NewClient(plans planClient) *Client {
	return &Client{plans: plans}
}

// GetOnboardingPlans returns the intern's onboarding tracks with the step
// each track is currently on. A finished track carries a nil current step.
func (c *Client) GetOnboardingPlans(ctx context.Context, userID int64, token string) ([]entity.OnboardingPlan, error) {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.MD{}
	}
	md.Set("authorization", "Bearer "+token)
	if id := observability.RequestID(ctx); id != "" {
		md.Set("x-request-id", id)
	}
	ctx = metadata.NewOutgoingContext(ctx, md)

	planContext, err := c.plans.GetPlanContext(ctx, &planv1.GetPlanContextRequest{UserId: userID})
	if err != nil {
		return nil, fmt.Errorf("get plan context: %w", err)
	}

	plans := make([]entity.OnboardingPlan, 0, len(planContext.GetTracks()))
	for _, track := range planContext.GetTracks() {
		plan := entity.OnboardingPlan{
			ID:    track.GetTrack(),
			Title: track.GetDisplayName(),
		}
		if step := track.GetCurrentStep(); step != nil {
			plan.CurrentStep = &entity.OnboardingStep{
				ID:         step.GetId(),
				Title:      step.GetTitle(),
				ResourceID: step.GetResourceId(),
				ActionType: step.GetActionType(),
			}
		}
		plans = append(plans, plan)
	}
	return plans, nil
}
