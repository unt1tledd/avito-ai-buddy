-- +goose Up
CREATE SCHEMA IF NOT EXISTS chat;

CREATE TABLE chat.conversation (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE chat.conversation;
