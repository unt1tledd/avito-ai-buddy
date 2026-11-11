-- name: LockTrackTemplate :one
SELECT id FROM onboarding.step_template
WHERE track = sqlc.arg(track) AND track NOT IN ('', 'common')
ORDER BY id
LIMIT 1
FOR SHARE;

-- name: AssignPlan :one
WITH plan AS (
    INSERT INTO onboarding.plan (user_id, track)
    VALUES (sqlc.arg(user_id), sqlc.arg(track))
    ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
    WHERE onboarding.plan.track = EXCLUDED.track
    RETURNING id, user_id, track
), assigned AS (
    INSERT INTO onboarding.plan_step (plan_id, step_id)
    SELECT plan.id, template.id
    FROM plan
    JOIN onboarding.step_template AS template ON template.track IN ('common', plan.track)
    ON CONFLICT (plan_id, step_id) DO NOTHING
)
SELECT id, user_id, track FROM plan;

-- name: GetPlanByUserID :one
SELECT id, user_id, track FROM onboarding.plan WHERE user_id = $1;
