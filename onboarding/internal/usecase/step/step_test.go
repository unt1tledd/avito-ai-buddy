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
	plan := entity.Plan{ID: 7, UserID: 42}
	current := &entity.Step{ID: "intro", Status: entity.StepPending}
	dbErr := errors.New("database unavailable")

	for _, tc := range []struct {
		name    string
		userID  int64
		planErr error
		stepErr error
		want    *entity.Step
		wantErr error
	}{
		{name: "pending step", userID: plan.UserID, want: current},
		{name: "all steps completed", userID: plan.UserID},
		{name: "plan not assigned", userID: plan.UserID, planErr: onboardingerrors.ErrPlanNotFound},
		{name: "invalid user", wantErr: onboardingerrors.ErrInvalidUserID},
		{name: "plan lookup failed", userID: plan.UserID, planErr: dbErr, wantErr: dbErr},
		{name: "step lookup failed", userID: plan.UserID, stepErr: dbErr, wantErr: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			ctrl := gomock.NewController(t)
			plans := mocks.NewMockplanRepository(ctrl)
			steps := mocks.NewMockstepRepository(ctrl)
			if tc.userID > 0 {
				plans.EXPECT().GetPlanByUserID(ctx, tc.userID).Return(plan, tc.planErr)
				if tc.planErr == nil {
					steps.EXPECT().GetCurrentStep(ctx, plan.ID).Return(tc.want, tc.stepErr)
				}
			}

			got, err := step.NewStepService(plans, steps).GetCurrentStep(ctx, tc.userID)
			if !errors.Is(err, tc.wantErr) || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("GetCurrentStep = %+v, %v; want %+v, %v", got, err, tc.want, tc.wantErr)
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

func TestReopenStep(t *testing.T) {
	for _, tc := range []struct {
		name     string
		userID   int64
		stepID   string
		repoCall bool
		repoErr  error
		wantErr  error
	}{
		{name: "reopen assigned step", userID: 42, stepID: "intro", repoCall: true},
		{name: "step not assigned", userID: 42, stepID: "intro", repoCall: true, repoErr: onboardingerrors.ErrStepNotFound, wantErr: onboardingerrors.ErrStepNotFound},
		{name: "blank step ID", userID: 42, stepID: " \t", wantErr: onboardingerrors.ErrInvalidStepID},
		{name: "invalid user", stepID: "intro", wantErr: onboardingerrors.ErrInvalidUserID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			steps := mocks.NewMockstepRepository(gomock.NewController(t))
			if tc.repoCall {
				steps.EXPECT().ReopenStep(ctx, tc.userID, tc.stepID).Return(tc.repoErr)
			}
			err := step.NewStepService(nil, steps).ReopenStep(ctx, tc.userID, tc.stepID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ReopenStep error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestUpdateStepTemplate(t *testing.T) {
	ctx := t.Context()
	template := entity.StepTemplate{Key: "intro", Title: "Team intro", Period: "Week 1", Position: 5}
	steps := mocks.NewMockstepRepository(gomock.NewController(t))
	steps.EXPECT().UpdateStepTemplate(ctx, template).Return(onboardingerrors.ErrStepTemplateNotFound)

	err := step.NewStepService(nil, steps).UpdateStepTemplate(ctx, template)
	if !errors.Is(err, onboardingerrors.ErrStepTemplateNotFound) {
		t.Fatalf("UpdateStepTemplate error = %v, want %v", err, onboardingerrors.ErrStepTemplateNotFound)
	}
}
