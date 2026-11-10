-- name: GetPlanSteps :many
SELECT template.step_key AS id, template.title, template.description, template.period, template.resource_id, step.completed_at
FROM onboarding.plan_step AS step
JOIN onboarding.step_template AS template ON template.id = step.step_id
WHERE step.plan_id = $1
ORDER BY template.position, template.id;

-- name: GetCurrentStep :one
SELECT template.step_key AS id, template.title, template.description, template.period, template.resource_id
FROM onboarding.plan_step AS step
JOIN onboarding.step_template AS template ON template.id = step.step_id
WHERE step.plan_id = $1 AND step.completed_at IS NULL
ORDER BY template.position, template.id
LIMIT 1;

-- name: MarkStepCompleted :one
UPDATE onboarding.plan_step AS step
SET completed_at = now()
FROM onboarding.plan AS plan, onboarding.step_template AS template
WHERE step.plan_id = plan.id AND plan.user_id = sqlc.arg(user_id)
  AND step.step_id = template.id AND template.step_key = sqlc.arg(step_key)
RETURNING template.step_key;

-- name: ReopenStep :one
UPDATE onboarding.plan_step AS step
SET completed_at = NULL
FROM onboarding.plan AS plan, onboarding.step_template AS template
WHERE step.plan_id = plan.id AND plan.user_id = sqlc.arg(user_id)
  AND step.step_id = template.id AND template.step_key = sqlc.arg(step_key)
RETURNING template.step_key;

-- name: UpdateStepTemplate :one
UPDATE onboarding.step_template
SET title = sqlc.arg(title),
    description = sqlc.arg(description),
    period = sqlc.arg(period),
    position = sqlc.arg(position),
    resource_id = sqlc.arg(resource_id)
WHERE step_key = sqlc.arg(step_key)
RETURNING step_key;
