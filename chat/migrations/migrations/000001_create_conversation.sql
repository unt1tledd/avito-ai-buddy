-- +goose Up
CREATE SCHEMA IF NOT EXISTS chat;

CREATE TABLE chat.conversation (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    title text NOT NULL DEFAULT 'Новый чат',
    deleted_at timestamptz,
    is_default boolean NOT NULL DEFAULT true
);

CREATE UNIQUE INDEX conversation_default_user
    ON chat.conversation (user_id) WHERE is_default AND deleted_at IS NULL;

-- +goose Down
DROP TABLE chat.conversation;
