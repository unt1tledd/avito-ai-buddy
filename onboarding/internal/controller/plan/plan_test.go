package plan_test

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"avito-ai-buddy/onboarding/internal/controller/plan"
	"avito-ai-buddy/onboarding/internal/controller/plan/mocks"
	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	pb "avito-ai-buddy/pkg/generated/plan/v1"
	steppb "avito-ai-buddy/pkg/generated/step/v1"
)

func TestPlanController(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 42
	stored := entity.Plan{ID: 7, UserID: userID, Track: "qa", Steps: []entity.Step{
		{ID: "intro", Title: "Meet the team", Status: entity.StepCompleted},
	}}
	expected := &pb.GetPlanResponse{Id: 7, UserId: userID, Track: "qa", Steps: []*steppb.Step{
		{Id: "intro", Title: "Meet the team", Status: "completed"},
	}}

	for _, tc := range []struct {
		name       string
		userID     int64
		serviceErr error
		code       codes.Code
	}{
		{name: "success", userID: userID},
		{name: "validation", code: codes.InvalidArgument},
		{name: "track conflict", userID: userID, serviceErr: onboardingerrors.ErrPlanTrackConflict, code: codes.AlreadyExists},
	} {
		t.Run("AssignPlan/"+tc.name, func(t *testing.T) {
			service := mocks.NewMockplanService(gomock.NewController(t))
			if tc.userID != 0 {
				service.EXPECT().AssignPlan(ctx, tc.userID, "qa").Return(stored, tc.serviceErr)
			}
			result, err := plan.NewPlanController(service, zap.NewNop()).AssignPlan(ctx, &pb.AssignPlanRequest{UserId: tc.userID, Track: "qa"})
			checkPlanResponse(t, result, err, expected, tc.code)
		})
	}

	for _, tc := range []struct {
		name       string
		userID     int64
		serviceErr error
		code       codes.Code
	}{
		{name: "success", userID: userID},
		{name: "validation", code: codes.InvalidArgument},
		{name: "missing plan", userID: userID, serviceErr: onboardingerrors.ErrPlanNotFound, code: codes.NotFound},
	} {
		t.Run("GetPlan/"+tc.name, func(t *testing.T) {
			service := mocks.NewMockplanService(gomock.NewController(t))
			if tc.userID != 0 {
				service.EXPECT().GetPlan(ctx, tc.userID).Return(stored, tc.serviceErr)
			}
			result, err := plan.NewPlanController(service, zap.NewNop()).GetPlan(ctx, &pb.GetPlanRequest{UserId: tc.userID})
			checkPlanResponse(t, result, err, expected, tc.code)
		})
	}
}

func checkPlanResponse(t *testing.T, result *pb.GetPlanResponse, err error, expected *pb.GetPlanResponse, code codes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Fatalf("error = %v, want status %s", err, code)
	}
	if code != codes.OK {
		expected = nil
	}
	if !proto.Equal(result, expected) {
		t.Errorf("response = %v, want %v", result, expected)
	}
}
