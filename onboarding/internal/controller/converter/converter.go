package converter

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	planpb "avito-ai-buddy/pkg/generated/plan/v1"
	steppb "avito-ai-buddy/pkg/generated/step/v1"
)

func PlanToProto(plan entity.Plan) *planpb.GetPlanResponse {
	result := &planpb.GetPlanResponse{Id: plan.ID, UserId: plan.UserID, Track: plan.Track}
	for _, step := range plan.Steps {
		result.Steps = append(result.Steps, StepToProto(step))
	}
	return result
}

func StepToProto(step entity.Step) *steppb.Step {
	return &steppb.Step{
		Id: step.ID, Title: step.Title, Description: step.Description,
		Period: step.Period, ResourceId: step.ResourceID, Status: string(step.Status),
	}
}

func StatusError(err error) error {
	switch {
	case errors.Is(err, onboardingerrors.ErrInvalidUserID),
		errors.Is(err, onboardingerrors.ErrInvalidStepID),
		errors.Is(err, onboardingerrors.ErrUnsupportedTrack):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, onboardingerrors.ErrPlanTrackConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, onboardingerrors.ErrPlanNotFound),
		errors.Is(err, onboardingerrors.ErrStepNotFound),
		errors.Is(err, onboardingerrors.ErrStepTemplateNotFound),
		errors.Is(err, onboardingerrors.ErrUserMetadataNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, onboardingerrors.ErrUserUnavailable), errors.Is(err, onboardingerrors.ErrInvalidUserMetadata):
		return status.Error(codes.Unavailable, "user internship metadata unavailable")
	default:
		if ctxStatus := status.FromContextError(err); ctxStatus.Code() != codes.Unknown {
			return ctxStatus.Err()
		}
		return status.Error(codes.Internal, "onboarding operation failed")
	}
}
