package step_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.uber.org/mock/gomock"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	"avito-ai-buddy/onboarding/internal/usecase/step"
	"avito-ai-buddy/onboarding/internal/usecase/step/mocks"
)

func TestGetCurrentStep(t *testing.T) {
	ctx := context.Background()
	plan := entity.Plan{ID: 7, UserID: 42}
	current := &entity.Step{ID: "intro", Status: entity.StepPending}

	for _, tc := range []struct {
		name    string
		planErr error
		want    *entity.Step
	}{
		{name: "pending step", want: current},
		{name: "all steps completed"},
		{name: "plan not assigned", planErr: onboardingerrors.ErrPlanNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			plans := mocks.NewMockplanRepository(ctrl)
			steps := mocks.NewMockstepRepository(ctrl)
			plans.EXPECT().GetPlanByUserID(ctx, plan.UserID).Return(plan, tc.planErr)
			if tc.planErr == nil {
				steps.EXPECT().GetCurrentStep(ctx, plan.ID).Return(tc.want, nil)
			}

			got, err := step.NewStepService(plans, steps).GetCurrentStep(ctx, plan.UserID)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("GetCurrentStep = %+v, %v; want %+v, nil", got, err, tc.want)
			}
		})
	}
}

func TestMarkStepCompleted(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		userID   int64
		stepID   string
		repoCall bool
		wantErr  error
	}{
		{name: "complete assigned step", userID: 42, stepID: "intro", repoCall: true},
		{name: "step not assigned", userID: 42, stepID: "intro", repoCall: true, wantErr: onboardingerrors.ErrStepNotFound},
		{name: "blank step ID", userID: 42, stepID: " \t", wantErr: onboardingerrors.ErrInvalidStepID},
		{name: "invalid user", stepID: "intro", wantErr: onboardingerrors.ErrInvalidUserID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := mocks.NewMockstepRepository(gomock.NewController(t))
			if tc.repoCall {
				steps.EXPECT().MarkStepCompleted(ctx, tc.userID, tc.stepID).Return(tc.wantErr)
			}
			err := step.NewStepService(nil, steps).MarkStepCompleted(ctx, tc.userID, tc.stepID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("MarkStepCompleted error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
