-- +goose Up
CREATE SCHEMA task;

CREATE TABLE task.access_request (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    proposal_id bigint NOT NULL UNIQUE CHECK (proposal_id > 0),
    user_id bigint NOT NULL CHECK (user_id > 0),
    team_id bigint NOT NULL CHECK (team_id > 0),
    resource_id text NOT NULL CHECK (btrim(resource_id) <> ''),
    access_level text NOT NULL CHECK (btrim(access_level) <> ''),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    created_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    decided_by_user_id bigint CHECK (decided_by_user_id > 0),
    decision_comment text NOT NULL DEFAULT '',
    external_request_id text NOT NULL DEFAULT '',
    CHECK (
        (status = 'pending' AND decided_at IS NULL AND decided_by_user_id IS NULL) OR
        (status <> 'pending' AND decided_at IS NOT NULL AND decided_by_user_id IS NOT NULL)
    )
);

CREATE INDEX access_request_pending_team_idx ON task.access_request (team_id, id)
    WHERE status = 'pending';

CREATE UNIQUE INDEX access_request_external_id_idx ON task.access_request (external_request_id)
    WHERE external_request_id <> '';

CREATE TABLE task.outbox (
    task_id bigint PRIMARY KEY REFERENCES task.access_request (id),
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz
);

CREATE INDEX outbox_unsent_idx ON task.outbox (created_at, task_id) WHERE sent_at IS NULL;

-- +goose Down
DROP TABLE task.outbox;
DROP TABLE task.access_request;
DROP SCHEMA task;
