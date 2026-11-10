-- +goose Up
CREATE TYPE chat.message_role AS ENUM ('unspecified', 'user', 'assistant');

CREATE TABLE chat.message (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES chat.conversation (id),
    role chat.message_role NOT NULL,
    body text NOT NULL,
    sources jsonb NOT NULL,
    created_at timestamptz DEFAULT NOW(),
    idempotency_key BIGINT,
    assistant_message_id BIGINT REFERENCES chat.message (id),
    reply_claim_until timestamptz,
    CONSTRAINT message_idempotency_key_unique UNIQUE (conversation_id, idempotency_key),
    CONSTRAINT message_user_idempotency_key_required
        CHECK (role <> 'user' OR (idempotency_key IS NOT NULL AND idempotency_key > 0))
);

CREATE INDEX message_history_idx ON chat.message (conversation_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS chat.message;
DROP TYPE  IF EXISTS chat.message_role;
