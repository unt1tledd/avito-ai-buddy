package step_test

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"avito-ai-buddy/onboarding/internal/controller/step"
	"avito-ai-buddy/onboarding/internal/controller/step/mocks"
	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	pb "avito-ai-buddy/pkg/generated/step/v1"
)

func TestUpdateStepTemplate(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		title      string
		position   int32
		serviceErr error
		want       codes.Code
	}{
		{name: "valid update", title: "Team intro", position: 5, want: codes.OK},
		{name: "missing template", title: "Team intro", position: 5, serviceErr: onboardingerrors.ErrStepTemplateNotFound, want: codes.NotFound},
		{name: "blank title", title: " \t", position: 5, want: codes.InvalidArgument},
		{name: "nonpositive position", title: "Team intro", want: codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := mocks.NewMockstepService(gomock.NewController(t))
			req := &pb.UpdateStepTemplateRequest{
				StepKey: "intro", Title: tc.title, Description: "Meet your team",
				Period: "Week 1", Position: tc.position, ResourceId: "guide",
			}
			if tc.want != codes.InvalidArgument {
				service.EXPECT().UpdateStepTemplate(ctx, entity.StepTemplate{
					Key: "intro", Title: tc.title, Description: "Meet your team",
					Period: "Week 1", Position: tc.position, ResourceID: "guide",
				}).Return(tc.serviceErr)
			}

			resp, err := step.NewStepController(service, zap.NewNop()).UpdateStepTemplate(ctx, req)
			if status.Code(err) != tc.want {
				t.Fatalf("UpdateStepTemplate error = %v, want code %v", err, tc.want)
			}
			if (resp != nil) != (tc.want == codes.OK) {
				t.Errorf("UpdateStepTemplate response = %v, want response only on success", resp)
			}
		})
	}
}

func TestGetCurrentStep(t *testing.T) {
	ctx := context.Background()
	current := &entity.Step{ID: "intro", Title: "Intro", Status: entity.StepPending}
	for _, tc := range []struct {
		name       string
		userID     int64
		current    *entity.Step
		serviceErr error
		code       codes.Code
	}{
		{name: "pending step", userID: 42, current: current},
		{name: "all steps completed", userID: 42},
		{name: "invalid user", code: codes.InvalidArgument},
		{name: "canceled", userID: 42, serviceErr: context.Canceled, code: codes.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := mocks.NewMockstepService(gomock.NewController(t))
			if tc.userID != 0 {
				service.EXPECT().GetCurrentStep(ctx, tc.userID).Return(tc.current, tc.serviceErr)
			}
			result, err := step.NewStepController(service, zap.NewNop()).GetCurrentStep(ctx, &pb.GetCurrentStepRequest{UserId: tc.userID})
			if status.Code(err) != tc.code {
				t.Fatalf("error = %v, want status %s", err, tc.code)
			}
			if (result != nil) != (tc.code == codes.OK) {
				t.Fatalf("unexpected response: %v", result)
			}
			got := result.GetStep()
			if tc.current == nil {
				if got != nil {
					t.Errorf("step = %v, want nil", got)
				}
			} else if got.GetId() != "intro" || got.GetTitle() != "Intro" || got.GetStatus() != "pending" {
				t.Errorf("unexpected current step: %v", got)
			}
		})
	}
}

func TestCompleteStep(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 42
	for _, tc := range []struct {
		name       string
		stepID     string
		serviceErr error
		code       codes.Code
	}{
		{name: "complete", stepID: "intro"},
		{name: "missing step", stepID: "intro", serviceErr: onboardingerrors.ErrStepNotFound, code: codes.NotFound},
		{name: "blank ID", stepID: " ", code: codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := mocks.NewMockstepService(gomock.NewController(t))
			if tc.code != codes.InvalidArgument {
				service.EXPECT().MarkStepCompleted(ctx, userID, tc.stepID).Return(tc.serviceErr)
			}
			result, err := step.NewStepController(service, zap.NewNop()).CompleteStep(ctx, &pb.CompleteStepRequest{UserId: userID, StepId: tc.stepID})
			if status.Code(err) != tc.code {
				t.Fatalf("error = %v, want status %s", err, tc.code)
			}
			if tc.code == codes.OK {
				if result.GetStepId() != tc.stepID || result.GetStatus() != "completed" {
					t.Errorf("unexpected completion: %v", result)
				}
			} else if result != nil {
				t.Errorf("response = %v, want nil", result)
			}
		})
	}
}

func TestReopenStep(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stepID     string
		stepStatus string
		serviceErr error
		want       codes.Code
	}{
		{name: "reopen", stepID: "intro", stepStatus: "pending", want: codes.OK},
		{name: "missing step", stepID: "intro", stepStatus: "pending", serviceErr: onboardingerrors.ErrStepNotFound, want: codes.NotFound},
		{name: "blank step ID", stepID: " ", stepStatus: "pending", want: codes.InvalidArgument},
		{name: "missing status", stepID: "intro", want: codes.InvalidArgument},
		{name: "unsupported status", stepID: "intro", stepStatus: "completed", want: codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			const userID int64 = 42
			service := mocks.NewMockstepService(gomock.NewController(t))
			if tc.want != codes.InvalidArgument {
				service.EXPECT().ReopenStep(ctx, userID, tc.stepID).Return(tc.serviceErr)
			}
			controller := step.NewStepController(service, zap.NewNop())
			result, err := controller.ReopenStep(ctx, &pb.ReopenStepRequest{UserId: userID, StepId: tc.stepID, Status: tc.stepStatus})
			if code := status.Code(err); code != tc.want {
				t.Fatalf("ReopenStep error = %v, want %v", err, tc.want)
			}
			if tc.want == codes.OK && (result.GetStepId() != tc.stepID || result.GetStatus() != "pending") {
				t.Errorf("unexpected reopen response: %v", result)
			}
		})
	}
}
