package plan

import (
	"context"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"avito-ai-buddy/onboarding/internal/controller/converter"
	"avito-ai-buddy/onboarding/internal/entity"
	pb "avito-ai-buddy/pkg/generated/plan/v1"
)

//go:generate mockgen -source=plan.go -destination=mocks/plan_mocks.go -package=mocks

type planService interface {
	AssignPlan(ctx context.Context, userID int64, track string) (entity.Plan, error)
	GetPlan(ctx context.Context, userID int64) (entity.Plan, error)
}

type PlanController struct {
	planService planService
	logger      *zap.Logger
}

func NewPlanController(planService planService, logger *zap.Logger) *PlanController {
	return &PlanController{planService: planService, logger: logger}
}

func (s *PlanController) AssignPlan(ctx context.Context, req *pb.AssignPlanRequest) (*pb.GetPlanResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("onboarding request validation failed", zap.String("rpc", "AssignPlan"), zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	plan, err := s.planService.AssignPlan(ctx, req.GetUserId(), req.GetTrack())
	if err != nil {
		s.logger.Warn("create onboarding plan", zap.Error(err))
		return nil, converter.StatusError(err)
	}
	return converter.PlanToProto(plan), nil
}

func (s *PlanController) GetPlan(ctx context.Context, req *pb.GetPlanRequest) (*pb.GetPlanResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("onboarding request validation failed", zap.String("rpc", "GetPlan"), zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	plan, err := s.planService.GetPlan(ctx, req.GetUserId())
	if err != nil {
		s.logger.Warn("get onboarding plan", zap.Error(err))
		return nil, converter.StatusError(err)
	}
	return converter.PlanToProto(plan), nil
}
