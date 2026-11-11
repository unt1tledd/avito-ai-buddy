package step_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/mock/gomock"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	"avito-ai-buddy/onboarding/internal/repository/step"
	"avito-ai-buddy/onboarding/internal/repository/step/mocks"
	sqlc "avito-ai-buddy/onboarding/internal/repository/step/sqlc"
)

func TestUpdateStepTemplate(t *testing.T) {
	ctx := context.Background()
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name     string
		queryErr error
		wantErr  error
	}{
		{name: "update template"},
		{name: "missing template", queryErr: pgx.ErrNoRows, wantErr: onboardingerrors.ErrStepTemplateNotFound},
		{name: "database error", queryErr: dbErr, wantErr: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries := mocks.NewMockQuerier(gomock.NewController(t))
			queries.EXPECT().UpdateStepTemplate(ctx, sqlc.UpdateStepTemplateParams{
				StepKey: "intro", Title: "Intro", Description: "Meet your team",
				Period: "Week 1", Position: 5, ResourceID: "guide",
			}).Return("intro", tc.queryErr)

			err := step.NewStepRepository(queries).UpdateStepTemplate(ctx, entity.StepTemplate{
				Key: "intro", Title: "Intro", Description: "Meet your team",
				Period: "Week 1", Position: 5, ResourceID: "guide",
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("UpdateStepTemplate error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGetPlanSteps(t *testing.T) {
	ctx := context.Background()
	const planID int64 = 7
	queries := mocks.NewMockQuerier(gomock.NewController(t))
	queries.EXPECT().GetPlanSteps(ctx, planID).Return([]sqlc.GetPlanStepsRow{
		{ID: "intro", Title: "Intro"},
		{ID: "access", Title: "Access", CompletedAt: pgtype.Timestamptz{Valid: true}},
	}, nil)
	result, err := step.NewStepRepository(queries).GetPlanSteps(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	want := []entity.Step{
		{ID: "intro", Title: "Intro", Status: entity.StepPending},
		{ID: "access", Title: "Access", Status: entity.StepCompleted},
	}
	if !slices.Equal(result, want) {
		t.Errorf("steps = %+v, want %+v", result, want)
	}
}

func TestGetCurrentStep(t *testing.T) {
	ctx := context.Background()
	const planID int64 = 7
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name     string
		queryErr error
		wantErr  error
	}{
		{name: "pending step"},
		{name: "no pending step", queryErr: pgx.ErrNoRows},
		{name: "database error", queryErr: dbErr, wantErr: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries := mocks.NewMockQuerier(gomock.NewController(t))
			queries.EXPECT().GetCurrentStep(ctx, planID).Return(sqlc.GetCurrentStepRow{ID: "intro", Title: "Intro"}, tc.queryErr)
			result, err := step.NewStepRepository(queries).GetCurrentStep(ctx, planID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if tc.queryErr != nil {
				if result != nil {
					t.Errorf("step = %+v, want nil", result)
				}
				return
			}
			want := entity.Step{ID: "intro", Title: "Intro", Status: entity.StepPending}
			if result == nil || *result != want {
				t.Errorf("step = %+v, want %+v", result, want)
			}
		})
	}
}

func TestMarkStepCompleted(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		queryErr error
		wantErr  error
	}{
		{name: "complete"},
		{name: "step not assigned", queryErr: pgx.ErrNoRows, wantErr: onboardingerrors.ErrStepNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries := mocks.NewMockQuerier(gomock.NewController(t))
			queries.EXPECT().MarkStepCompleted(ctx, sqlc.MarkStepCompletedParams{UserID: 42, StepKey: "intro"}).Return("intro", tc.queryErr)
			if err := step.NewStepRepository(queries).MarkStepCompleted(ctx, 42, "intro"); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
