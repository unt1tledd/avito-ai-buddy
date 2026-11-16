package plan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.uber.org/mock/gomock"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	"avito-ai-buddy/onboarding/internal/repository/plan"
	"avito-ai-buddy/onboarding/internal/repository/plan/mocks"
	sqlc "avito-ai-buddy/onboarding/internal/repository/plan/sqlc"
)

func TestGetPlanByUserID(t *testing.T) {
	ctx := context.Background()
	dbErr := errors.New("database unavailable")
	for _, tc := range []struct {
		name     string
		row      sqlc.OnboardingPlan
		queryErr error
		want     entity.Plan
		wantErr  error
	}{
		{name: "existing plan", row: sqlc.OnboardingPlan{ID: 7, UserID: 42, Track: "qa"}, want: entity.Plan{ID: 7, UserID: 42, Track: "qa"}},
		{name: "missing plan", queryErr: pgx.ErrNoRows, wantErr: onboardingerrors.ErrPlanNotFound},
		{name: "database error", queryErr: dbErr, wantErr: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			queries := mocks.NewMockQuerier(gomock.NewController(t))
			queries.EXPECT().GetPlanByUserID(ctx, int64(42)).Return(tc.row, tc.queryErr)
			result, err := plan.NewPlanRepository(queries, nil).GetPlanByUserID(ctx, 42)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if result.ID != tc.want.ID || result.UserID != tc.want.UserID || result.Track != tc.want.Track || len(result.Steps) != 0 {
				t.Errorf("plan = %+v, want %+v", result, tc.want)
			}
		})
	}
}
