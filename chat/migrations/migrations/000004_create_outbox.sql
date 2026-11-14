-- +goose Up
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TYPE chat.outbox_status as ENUM ('CREATED', 'IN_PROGRESS', 'SUCCESS', 'FAILED');

CREATE TABLE chat.outbox
(
    idempotency_key TEXT PRIMARY KEY,
    data            JSONB                   NOT NULL,
    status          chat.outbox_status      NOT NULL,
    kind            INT                     NOT NULL,
    attempts        INT                     NOT NULL DEFAULT 0,
    created_at      TIMESTAMP DEFAULT now() NOT NULL,
    updated_at      TIMESTAMP DEFAULT now() NOT NULL,
    processed_at    TIMESTAMPTZ
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION chat.update_outbox_timestamp() RETURNS TRIGGER AS
$$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd


CREATE TRIGGER trigger_update_outbox_timestamp
    BEFORE UPDATE
    ON chat.outbox
    FOR EACH ROW
EXECUTE FUNCTION chat.update_outbox_timestamp();


-- +goose Down
DROP TRIGGER IF EXISTS trigger_update_outbox_timestamp ON chat.outbox;
DROP FUNCTION IF EXISTS chat.update_outbox_timestamp();
DROP TABLE IF EXISTS chat.outbox;
DROP TYPE IF EXISTS chat.outbox_status;
