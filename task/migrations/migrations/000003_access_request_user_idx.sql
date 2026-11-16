-- +goose Up
CREATE INDEX access_request_user_idx ON task.access_request (user_id, id);

-- +goose Down
DROP INDEX task.access_request_user_idx;
