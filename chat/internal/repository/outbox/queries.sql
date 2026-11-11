-- name: SendOutboxMessage :exec
INSERT INTO chat.outbox (idempotency_key, data, status, kind)
VALUES (
           sqlc.arg(idempotency_key),
           sqlc.arg(data),
           'CREATED'::chat.outbox_status,
           sqlc.arg(kind)
       )
ON CONFLICT (idempotency_key) DO NOTHING;

-- name: GetOutboxMessage :one
SELECT status, attempts
FROM chat.outbox
WHERE idempotency_key = sqlc.arg(idempotency_key);

-- name: GetOutboxMessages :many
UPDATE chat.outbox
SET status = 'IN_PROGRESS'::chat.outbox_status
WHERE idempotency_key IN (
    SELECT idempotency_key
    FROM chat.outbox
    WHERE
        status = 'CREATED'::chat.outbox_status
       OR (
        status = 'IN_PROGRESS'::chat.outbox_status
            AND updated_at < now() - sqlc.arg(in_progress_ttl)::interval
        )
    ORDER BY created_at, idempotency_key
    LIMIT sqlc.arg(batch_size)::integer
        FOR UPDATE SKIP LOCKED
)
RETURNING idempotency_key, data, kind, attempts;

-- name: MarkOutboxMessagesAsProcessed :exec
UPDATE chat.outbox
SET status = 'SUCCESS'::chat.outbox_status,
    processed_at = now()
WHERE idempotency_key = ANY(sqlc.arg(idempotency_keys)::text[]);

-- name: MarkOutboxMessagesAsRetryable :exec
UPDATE chat.outbox
SET status = 'CREATED'::chat.outbox_status,
    attempts = attempts + 1
WHERE idempotency_key = ANY(sqlc.arg(idempotency_keys)::text[]);

-- name: MarkOutboxMessagesAsFailed :exec
UPDATE chat.outbox
SET status = 'FAILED'::chat.outbox_status,
    attempts = attempts + 1,
    processed_at = now()
WHERE idempotency_key = ANY(sqlc.arg(idempotency_keys)::text[]);

-- name: ResetOutboxMessage :exec
UPDATE chat.outbox
SET status = 'CREATED'::chat.outbox_status,
    attempts = 0,
    processed_at = NULL
WHERE idempotency_key = sqlc.arg(idempotency_key);
