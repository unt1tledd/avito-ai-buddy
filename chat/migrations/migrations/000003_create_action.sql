-- +goose Up
CREATE TABLE chat.action (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    message_id BIGINT NOT NULL UNIQUE REFERENCES chat.message (id),
    resource_id text NOT NULL,
    resource_label text NOT NULL,
    access_level text NOT NULL,
    task_id text NOT NULL DEFAULT '',
    external_request_id text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS chat.action;
