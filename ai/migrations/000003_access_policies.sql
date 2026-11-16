-- +goose Up

ALTER TABLE documents ADD COLUMN access_policies JSONB NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(access_policies) = 'array');

-- +goose Down

ALTER TABLE documents DROP COLUMN access_policies;
