-- +goose Up
CREATE TABLE onboarding.step_template (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    step_key TEXT NOT NULL UNIQUE,
    track TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    period TEXT NOT NULL,
    position INTEGER NOT NULL,
    resource_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE onboarding.plan_step (
    plan_id BIGINT NOT NULL REFERENCES onboarding.plan(id) ON DELETE CASCADE,
    step_id BIGINT NOT NULL REFERENCES onboarding.step_template(id),
    completed_at TIMESTAMPTZ,
    PRIMARY KEY (plan_id, step_id)
);

-- +goose StatementBegin
CREATE FUNCTION onboarding.preserve_step_completed_at() RETURNS TRIGGER AS
$$
BEGIN
    NEW.completed_at := OLD.completed_at;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER preserve_step_completed_at
BEFORE UPDATE OF completed_at ON onboarding.plan_step
FOR EACH ROW
WHEN (OLD.completed_at IS NOT NULL AND NEW.completed_at IS NOT NULL)
EXECUTE FUNCTION onboarding.preserve_step_completed_at();

-- +goose Down
DROP TABLE IF EXISTS onboarding.plan_step;
DROP FUNCTION IF EXISTS onboarding.preserve_step_completed_at();
DROP TABLE onboarding.step_template;
