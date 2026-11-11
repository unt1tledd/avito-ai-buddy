-- name: TryInsertInboxMessage :one
INSERT INTO chat.inbox (idempotency_key, kind, payload)
VALUES (sqlc.arg(idempotency_key), sqlc.arg(kind), sqlc.arg(payload))
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key;

-- name: CleanupInboxMessages :exec
DELETE FROM chat.inbox
WHERE processed_at < now() - sqlc.arg(retention)::interval;
