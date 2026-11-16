-- name: GetByProposal :one
SELECT * FROM task.access_request WHERE proposal_id = $1;

-- name: CreateAccessRequest :one
INSERT INTO task.access_request (proposal_id, user_id, team_id, resource_id, access_level)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (proposal_id) DO NOTHING
RETURNING *;

-- name: ListPending :many
SELECT * FROM task.access_request
WHERE team_id = sqlc.arg(team_id) AND status = 'pending' AND id > sqlc.arg(after_id)
ORDER BY id
LIMIT sqlc.arg(page_size);

-- name: GetUserTasks :many
SELECT * FROM task.access_request
WHERE user_id = sqlc.arg(user_id) AND id > sqlc.arg(after_id)
ORDER BY id
LIMIT sqlc.arg(page_size);

-- name: GetByTeam :one
SELECT * FROM task.access_request WHERE id = $1 AND team_id = $2;

-- name: Decide :one
UPDATE task.access_request
SET status = sqlc.arg(status), decided_at = now(), decided_by_user_id = sqlc.arg(decided_by_user_id),
    decision_comment = sqlc.arg(decision_comment), external_request_id = sqlc.arg(external_request_id)
WHERE id = sqlc.arg(id) AND team_id = sqlc.arg(team_id) AND status = 'pending'
RETURNING *;

-- name: InsertEvent :exec
INSERT INTO task.outbox (task_id, payload) VALUES ($1, $2);
