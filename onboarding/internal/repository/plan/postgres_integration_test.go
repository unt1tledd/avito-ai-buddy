package plan_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"avito-ai-buddy/onboarding/internal/entity"
	onboardingerrors "avito-ai-buddy/onboarding/internal/errors"
	"avito-ai-buddy/onboarding/internal/repository/plan"
	sqlc "avito-ai-buddy/onboarding/internal/repository/plan/sqlc"
	"avito-ai-buddy/onboarding/internal/repository/step"
	stepsqlc "avito-ai-buddy/onboarding/internal/repository/step/sqlc"
	"avito-ai-buddy/onboarding/migrations"
	"avito-ai-buddy/pkg/transactor"
)

func TestAssignPlanPostgres(t *testing.T) {
	dsn := os.Getenv("ONBOARDING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ONBOARDING_TEST_DATABASE_URL to a dedicated PostgreSQL test database")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := migrations.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	userID := rand.Int64N(1<<60) + 1
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM onboarding.plan WHERE user_id = $1", userID); err != nil {
			t.Errorf("clean test plan: %v", err)
		}
	})
	repo := plan.NewPlanRepository(sqlc.New(pool), transactor.NewTransactor(pool))

	for _, track := range []string{"common", ""} {
		if _, err := repo.AssignPlan(ctx, userID, track); !errors.Is(err, onboardingerrors.ErrUnsupportedTrack) {
			t.Fatalf("AssignPlan(%q) error = %v, want ErrUnsupportedTrack", track, err)
		}
	}
	if _, err := repo.GetPlanByUserID(ctx, userID); !errors.Is(err, onboardingerrors.ErrPlanNotFound) {
		t.Fatalf("GetPlanByUserID after failed assignment error = %v, want ErrPlanNotFound", err)
	}

	assigned, err := repo.AssignPlan(ctx, userID, "backend")
	if err != nil {
		t.Fatalf("assign plan: %v", err)
	}
	if assigned.ID <= 0 || assigned.UserID != userID || assigned.Track != "backend" {
		t.Fatalf("unexpected assigned plan: %+v", assigned)
	}
	repeated, err := repo.AssignPlan(ctx, userID, "backend")
	if err != nil || repeated.ID != assigned.ID {
		t.Fatalf("repeat assignment = %+v, %v; want plan ID %d", repeated, err, assigned.ID)
	}

	var common, backend, other int
	err = pool.QueryRow(ctx, `
  SELECT count(*) FILTER (WHERE template.track = 'common'),
         count(*) FILTER (WHERE template.track = 'backend'),
         count(*) FILTER (WHERE template.track NOT IN ('common', 'backend'))
  FROM onboarding.plan_step AS step
  JOIN onboarding.step_template AS template ON template.id = step.step_id
  WHERE step.plan_id = $1
 `, assigned.ID).Scan(&common, &backend, &other)
	if err != nil {
		t.Fatal(err)
	}
	if common == 0 || backend == 0 || other != 0 {
		t.Errorf("unexpected assigned steps: common=%d backend=%d other=%d", common, backend, other)
	}

	t.Run("complete and reopen step", func(t *testing.T) {
		queries := stepsqlc.New(pool)
		steps := step.NewStepRepository(queries)
		first, err := steps.GetCurrentStep(ctx, assigned.ID)
		if err != nil || first == nil {
			t.Fatalf("read first step: %v, %v", first, err)
		}
		if err := steps.MarkStepCompleted(ctx, userID, first.ID); err != nil {
			t.Fatalf("complete first step: %v", err)
		}
		rows, err := queries.GetPlanSteps(ctx, assigned.ID)
		if err != nil || len(rows) == 0 || rows[0].ID != first.ID || !rows[0].CompletedAt.Valid {
			t.Fatalf("first step has no completion timestamp: %v, %v", rows, err)
		}
		completedAt := rows[0].CompletedAt.Time
		if err := steps.MarkStepCompleted(ctx, userID, first.ID); err != nil {
			t.Fatalf("complete first step again: %v", err)
		}
		rows, err = queries.GetPlanSteps(ctx, assigned.ID)
		if err != nil || len(rows) == 0 || !rows[0].CompletedAt.Valid || !rows[0].CompletedAt.Time.Equal(completedAt) {
			t.Fatalf("repeat completion changed the original timestamp %v: %v, %v", completedAt, rows, err)
		}

		next, err := steps.GetCurrentStep(ctx, assigned.ID)
		if err != nil || next == nil || next.ID == first.ID {
			t.Fatalf("current step after completion: %v, %v", next, err)
		}

		for range 2 {
			if err := steps.ReopenStep(ctx, userID, first.ID); err != nil {
				t.Fatalf("reopen first step: %v", err)
			}
		}

		current, err := steps.GetCurrentStep(ctx, assigned.ID)
		if err != nil || current == nil || current.ID != first.ID {
			t.Fatalf("current step after reopen: %v, %v; want %s", current, err, first.ID)
		}
		list, err := steps.GetPlanSteps(ctx, assigned.ID)
		if err != nil || len(list) == 0 || list[0].ID != first.ID || list[0].Status != entity.StepPending {
			t.Fatalf("plan after reopening first step: %v, %v", list, err)
		}
	})
}
