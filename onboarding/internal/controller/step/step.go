package step

import (
	"context"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"avito-ai-buddy/onboarding/internal/controller/converter"
	"avito-ai-buddy/onboarding/internal/entity"
	pb "avito-ai-buddy/pkg/generated/step/v1"
)

//go:generate mockgen -source=step.go -destination=mocks/step_mocks.go -package=mocks

type stepService interface {
	GetCurrentStep(ctx context.Context, userID int64) (*entity.Step, error)
	MarkStepCompleted(ctx context.Context, userID int64, stepID string) error
	ReopenStep(ctx context.Context, userID int64, stepID string) error
	UpdateStepTemplate(ctx context.Context, template entity.StepTemplate) error
}

type StepController struct {
	stepService stepService
	logger      *zap.Logger
}

func NewStepController(stepService stepService, logger *zap.Logger) *StepController {
	return &StepController{stepService: stepService, logger: logger}
}

func (s *StepController) UpdateStepTemplate(ctx context.Context, req *pb.UpdateStepTemplateRequest) (*pb.UpdateStepTemplateResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("onboarding request validation failed", zap.String("rpc", "UpdateStepTemplate"), zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	template := entity.StepTemplate{
		Key: req.GetStepKey(), Title: req.GetTitle(), Description: req.GetDescription(),
		Period: req.GetPeriod(), Position: req.GetPosition(), ResourceID: req.GetResourceId(),
	}
	if err := s.stepService.UpdateStepTemplate(ctx, template); err != nil {
		s.logger.Warn("update onboarding step template", zap.Error(err))
		return nil, converter.StatusError(err)
	}
	return &pb.UpdateStepTemplateResponse{}, nil
}

func (s *StepController) GetCurrentStep(ctx context.Context, req *pb.GetCurrentStepRequest) (*pb.GetCurrentStepResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("onboarding request validation failed", zap.String("rpc", "GetCurrentStep"), zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	step, err := s.stepService.GetCurrentStep(ctx, req.GetUserId())
	if err != nil {
		s.logger.Warn("get current onboarding step", zap.Error(err))
		return nil, converter.StatusError(err)
	}
	result := &pb.GetCurrentStepResponse{}
	if step != nil {
		result.Step = converter.StepToProto(*step)
	}
	return result, nil
}

func (s *StepController) CompleteStep(ctx context.Context, req *pb.CompleteStepRequest) (*pb.CompleteStepResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("onboarding request validation failed", zap.String("rpc", "CompleteStep"), zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := s.stepService.MarkStepCompleted(ctx, req.GetUserId(), req.GetStepId()); err != nil {
		s.logger.Warn("complete onboarding step", zap.Error(err))
		return nil, converter.StatusError(err)
	}
	return &pb.CompleteStepResponse{StepId: req.GetStepId(), Status: string(entity.StepCompleted)}, nil
}

func (s *StepController) ReopenStep(ctx context.Context, req *pb.ReopenStepRequest) (*pb.ReopenStepResponse, error) {
	if err := req.Validate(); err != nil {
		s.logger.Warn("onboarding request validation failed", zap.String("rpc", "ReopenStep"), zap.Error(err))
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := s.stepService.ReopenStep(ctx, req.GetUserId(), req.GetStepId()); err != nil {
		s.logger.Warn("reopen onboarding step", zap.Error(err))
		return nil, converter.StatusError(err)
	}
	return &pb.ReopenStepResponse{StepId: req.GetStepId(), Status: string(entity.StepPending)}, nil
}
