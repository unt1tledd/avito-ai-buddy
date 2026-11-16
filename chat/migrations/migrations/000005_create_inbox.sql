-- +goose Up
CREATE TABLE chat.inbox (
    idempotency_key TEXT PRIMARY KEY,
    kind            INT         NOT NULL,
    payload         JSONB       NOT NULL,
    processed_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE chat.inbox;
