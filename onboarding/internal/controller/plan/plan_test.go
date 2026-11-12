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

func TestGetPlanContext(t *testing.T) {
	const userID int64 = 42
	tracks := []entity.TrackContext{
		{
			Track:       "common",
			DisplayName: "Общая адаптация",
			CurrentStep: &entity.Step{
				ID: "common-06", Title: "Запросить доступ",
				ResourceID: "cluster-space", Status: entity.StepPending,
			},
		},
		{Track: "backend", DisplayName: "Backend"},
	}
	expected := &pb.GetPlanContextResponse{Tracks: []*pb.PlanTrackContext{
		{
			Track:       "common",
			DisplayName: "Общая адаптация",
			CurrentStep: &steppb.Step{
				Id: "common-06", Title: "Запросить доступ", Status: "pending",
				ResourceId: "cluster-space", ActionType: "access_request",
			},
		},
		{Track: "backend", DisplayName: "Backend"},
	}}

	for _, tc := range []struct {
		name       string
		userID     int64
		tracks     []entity.TrackContext
		serviceErr error
		want       *pb.GetPlanContextResponse
		code       codes.Code
	}{
		{name: "active and completed tracks", userID: userID, tracks: tracks, want: expected},
		{name: "no assigned plan", userID: userID, want: &pb.GetPlanContextResponse{}},
		{name: "invalid user", userID: -1, code: codes.InvalidArgument},
		{name: "canceled", userID: userID, serviceErr: context.Canceled, code: codes.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			service := mocks.NewMockplanService(gomock.NewController(t))
			if tc.code != codes.InvalidArgument {
				service.EXPECT().GetPlanContext(ctx, tc.userID).Return(tc.tracks, tc.serviceErr)
			}

			result, err := plan.NewPlanController(service, zap.NewNop()).GetPlanContext(ctx, &pb.GetPlanContextRequest{UserId: tc.userID})
			if status.Code(err) != tc.code {
				t.Fatalf("error = %v, want status %s", err, tc.code)
			}
			if !proto.Equal(result, tc.want) {
				t.Errorf("response = %v, want %v", result, tc.want)
			}
		})
	}
}
