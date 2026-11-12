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
		ActionType: actionType(step),
	}
}

// actionType is set while resource_id is only used for access requests.
func actionType(step entity.Step) string {
	if step.ResourceID == "" {
		return ""
	}
	return entity.ActionTypeAccessRequest
}

func TrackContextToProto(context entity.TrackContext) *planpb.PlanTrackContext {
	result := &planpb.PlanTrackContext{Track: context.Track, DisplayName: context.DisplayName}
	if context.CurrentStep != nil {
		result.CurrentStep = StepToProto(*context.CurrentStep)
	}
	return result
}

func TrackContextsToProto(contexts []entity.TrackContext) *planpb.GetPlanContextResponse {
	result := &planpb.GetPlanContextResponse{}
	for _, context := range contexts {
		result.Tracks = append(result.Tracks, TrackContextToProto(context))
	}
	return result
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
