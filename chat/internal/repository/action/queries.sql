-- name: Create :one
INSERT INTO chat.action (message_id, resource_id, resource_label, access_level)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: GetByID :one
SELECT a.id, a.message_id, a.resource_id, a.resource_label, a.access_level,
       a.task_id, a.external_request_id, a.status
FROM chat.action a
JOIN chat.message m ON m.id = a.message_id
JOIN chat.conversation c ON c.id = m.conversation_id
WHERE a.id = $1 AND c.user_id = $2 AND c.deleted_at IS NULL
FOR UPDATE OF a;

-- name: SaveAccessRequest :execrows
UPDATE chat.action SET
task_id = $2, external_request_id = $3, status = $4
WHERE id = $1;
