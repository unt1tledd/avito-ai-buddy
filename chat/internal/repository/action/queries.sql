-- name: Create :one
INSERT INTO chat.action (message_id, resource_id, resource_label, access_level)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: GetByID :one
SELECT a.id, a.message_id, a.resource_id, a.resource_label, a.access_level,
       a.task_id, a.external_request_id, a.status, m.created_at
FROM chat.action a
JOIN chat.message m ON m.id = a.message_id
JOIN chat.conversation c ON c.id = m.conversation_id
WHERE a.id = $1 AND c.user_id = $2 AND c.deleted_at IS NULL
FOR UPDATE OF a;

-- name: SaveAccessRequest :execrows
UPDATE chat.action SET
task_id = $2, external_request_id = $3, status = $4
WHERE id = $1;

-- name: ApplyTaskStatus :one
UPDATE chat.action a
SET task_id = sqlc.arg(task_id),
    external_request_id = sqlc.arg(external_request_id),
    status = sqlc.arg(status)
FROM chat.message m
JOIN chat.conversation c ON c.id = m.conversation_id
WHERE m.id = a.message_id AND c.user_id = sqlc.arg(user_id)
  AND ((sqlc.arg(proposal_id)::bigint > 0 AND a.id = sqlc.arg(proposal_id))
       OR (sqlc.arg(proposal_id)::bigint = 0 AND a.task_id = sqlc.arg(task_id)))
  AND a.status IN ('pending_dispatch', 'pending', 'failed')
  AND (a.task_id = '' OR a.task_id = sqlc.arg(task_id))
  AND (
      sqlc.arg(status)::text IN ('approved', 'rejected')
      OR (
          coalesce((SELECT o.data->>'dispatch_id' FROM chat.outbox o
                    WHERE o.idempotency_key = a.id::text), '') = sqlc.arg(dispatch_id)::text
          AND (
              (sqlc.arg(status)::text = 'pending' AND a.status IN ('pending_dispatch', 'failed'))
              OR (sqlc.arg(status)::text = 'failed' AND a.status = 'pending_dispatch' AND a.task_id = '')
          )
      )
  )
RETURNING c.id AS conversation_id, (c.deleted_at IS NOT NULL)::boolean AS conversation_deleted;
