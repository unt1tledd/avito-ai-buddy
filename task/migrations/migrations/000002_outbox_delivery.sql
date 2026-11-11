-- +goose Up
ALTER TABLE task.outbox
    ADD COLUMN status text NOT NULL DEFAULT 'CREATED' CHECK (status IN ('CREATED', 'SUCCESS', 'FAILED')),
    ADD COLUMN attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now();

UPDATE task.outbox SET status = 'SUCCESS' WHERE sent_at IS NOT NULL;

DROP INDEX task.outbox_unsent_idx;
CREATE INDEX outbox_ready_idx ON task.outbox (next_attempt_at, created_at, task_id)
    WHERE status = 'CREATED';

-- +goose Down
DROP INDEX task.outbox_ready_idx;
ALTER TABLE task.outbox DROP COLUMN status, DROP COLUMN attempts, DROP COLUMN next_attempt_at;
CREATE INDEX outbox_unsent_idx ON task.outbox (created_at, task_id) WHERE sent_at IS NULL;
