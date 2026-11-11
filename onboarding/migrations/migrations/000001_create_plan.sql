-- +goose Up
CREATE SCHEMA IF NOT EXISTS onboarding;

CREATE TABLE onboarding.plan (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL UNIQUE CHECK (user_id > 0),
    track TEXT NOT NULL CHECK (track <> '')
);

-- +goose Down
DROP TABLE IF EXISTS onboarding.plan;
