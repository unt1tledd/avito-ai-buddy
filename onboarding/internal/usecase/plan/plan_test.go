package plan_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.uber.org/mock/gomock"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	"avito-ai-buddy/onboarding/internal/usecase/plan"
	"avito-ai-buddy/onboarding/internal/usecase/plan/mocks"
)

func TestAssignPlan(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 42
	stored := entity.Plan{ID: 7, UserID: userID, Track: "qa"}
	steps := []entity.Step{{ID: "intro", Status: entity.StepPending}}
	wantPlan := stored
	wantPlan.Steps = steps

	for _, tc := range []struct {
		name    string
		userID  int64
		track   string
		expect  func(*mocks.MockplanRepository, *mocks.MockstepRepository, *mocks.MockuserClient)
		want    entity.Plan
		wantErr error
	}{
		{
			name:   "normalize explicit track",
			userID: userID, track: " QA ", want: wantPlan,
			expect: func(plans *mocks.MockplanRepository, stepRepo *mocks.MockstepRepository, _ *mocks.MockuserClient) {
				gomock.InOrder(
					plans.EXPECT().AssignPlan(ctx, userID, "qa").Return(stored, nil),
					stepRepo.EXPECT().GetPlanSteps(ctx, stored.ID).Return(steps, nil),
				)
			},
		},
		{
			name:   "use existing plan when track omitted",
			userID: userID, want: wantPlan,
			expect: func(plans *mocks.MockplanRepository, stepRepo *mocks.MockstepRepository, _ *mocks.MockuserClient) {
				gomock.InOrder(
					plans.EXPECT().GetPlanByUserID(ctx, userID).Return(stored, nil),
					stepRepo.EXPECT().GetPlanSteps(ctx, stored.ID).Return(steps, nil),
				)
			},
		},
		{
			name:   "resolve track through User for a new plan",
			userID: userID, want: wantPlan,
			expect: func(plans *mocks.MockplanRepository, stepRepo *mocks.MockstepRepository, user *mocks.MockuserClient) {
				gomock.InOrder(
					plans.EXPECT().GetPlanByUserID(ctx, userID).Return(entity.Plan{}, onboardingerrors.ErrPlanNotFound),
					user.EXPECT().GetTrack(ctx, userID).Return(" QA ", nil),
					plans.EXPECT().AssignPlan(ctx, userID, "qa").Return(stored, nil),
					stepRepo.EXPECT().GetPlanSteps(ctx, stored.ID).Return(steps, nil),
				)
			},
		},
		{
			name:   "unsupported track",
			userID: userID, track: "unknown", wantErr: onboardingerrors.ErrUnsupportedTrack,
			expect: func(plans *mocks.MockplanRepository, _ *mocks.MockstepRepository, _ *mocks.MockuserClient) {
				plans.EXPECT().AssignPlan(ctx, userID, "unknown").Return(entity.Plan{}, onboardingerrors.ErrUnsupportedTrack)
			},
		},
		{name: "invalid user", wantErr: onboardingerrors.ErrInvalidUserID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			plans := mocks.NewMockplanRepository(ctrl)
			stepRepo := mocks.NewMockstepRepository(ctrl)
			user := mocks.NewMockuserClient(ctrl)
			if tc.expect != nil {
				tc.expect(plans, stepRepo, user)
			}

			got, err := plan.NewPlanService(plans, stepRepo, user).AssignPlan(ctx, tc.userID, tc.track)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AssignPlan error = %v, want %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("AssignPlan = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGetPlanLoadsSteps(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	plans := mocks.NewMockplanRepository(ctrl)
	steps := mocks.NewMockstepRepository(ctrl)
	stored := entity.Plan{ID: 9, UserID: 42, Track: "backend"}
	want := stored
	want.Steps = []entity.Step{{ID: "intro", Status: entity.StepCompleted}}
	gomock.InOrder(
		plans.EXPECT().GetPlanByUserID(ctx, stored.UserID).Return(stored, nil),
		steps.EXPECT().GetPlanSteps(ctx, stored.ID).Return(want.Steps, nil),
	)

	got, err := plan.NewPlanService(plans, steps, nil).GetPlan(ctx, stored.UserID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetPlan = %+v, %v; want %+v, nil", got, err, want)
	}
}
