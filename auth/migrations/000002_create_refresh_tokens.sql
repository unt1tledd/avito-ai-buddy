-- +goose Up
CREATE TABLE auth.refresh_tokens (
    user_id bigint NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,

    token_hash bytea PRIMARY KEY,
    expires_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE auth.refresh_tokens;
