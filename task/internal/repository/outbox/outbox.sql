-- name: GetMessages :many
SELECT task_id, payload, attempts FROM task.outbox
WHERE status = 'CREATED' AND next_attempt_at <= now()
ORDER BY next_attempt_at, created_at, task_id
LIMIT sqlc.arg(batch_size) FOR UPDATE SKIP LOCKED;

-- name: MarkAsProcessed :exec
UPDATE task.outbox SET status = 'SUCCESS', sent_at = now()
WHERE task_id = ANY(sqlc.arg(task_ids)::bigint[]);

-- name: MarkAsRetryable :exec
UPDATE task.outbox
SET attempts = attempts + 1,
    next_attempt_at = now() + sqlc.arg(retry_delay)::interval
WHERE task_id = ANY(sqlc.arg(task_ids)::bigint[]);

-- name: MarkAsFailed :exec
UPDATE task.outbox SET status = 'FAILED', attempts = attempts + 1
WHERE task_id = ANY(sqlc.arg(task_ids)::bigint[]);
