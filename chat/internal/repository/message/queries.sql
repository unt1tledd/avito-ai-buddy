-- name: Create :one
INSERT INTO chat.message (conversation_id, role, body, sources, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: CreateIdempotentMessage :one
INSERT INTO chat.message (conversation_id, role, body, sources, created_at, idempotency_key)
VALUES ($1, $2, $3, $4, $5, sqlc.arg(idempotency_key)::bigint)
ON CONFLICT (conversation_id, idempotency_key) DO NOTHING
RETURNING id;

-- name: GetOrCreateQuestion :one
INSERT INTO chat.message (conversation_id, role, body, sources, created_at, idempotency_key)
VALUES ($1, 'user', $2, 'null'::jsonb, $3, sqlc.arg(idempotency_key)::bigint)
ON CONFLICT (conversation_id, idempotency_key) DO UPDATE
SET idempotency_key = EXCLUDED.idempotency_key
RETURNING id, body;

-- name: ClaimReply :one
UPDATE chat.message
SET reply_claim_until = clock_timestamp() + sqlc.arg(lease_duration)::interval
WHERE id = sqlc.arg(question_id)
  AND assistant_message_id IS NULL
  AND (reply_claim_until IS NULL OR reply_claim_until <= clock_timestamp())
RETURNING reply_claim_until;

-- name: FinishReply :execrows
UPDATE chat.message
SET assistant_message_id = sqlc.narg(assistant_message_id), reply_claim_until = NULL
WHERE id = sqlc.arg(question_id) AND reply_claim_until = sqlc.arg(claim_until);

-- name: GetReply :one
SELECT m.id, m.conversation_id, m.role, m.body, m.sources, m.created_at,
       coalesce(a.id, 0)::bigint AS proposal_id,
       coalesce(a.resource_id, '')::text AS resource_id,
       coalesce(a.resource_label, '')::text AS resource_label,
       coalesce(a.access_level, '')::text AS access_level,
       coalesce(a.task_id, '')::text AS task_id,
       coalesce(a.external_request_id, '')::text AS external_request_id,
       coalesce(a.status, '')::text AS status
FROM chat.message q
JOIN chat.message m ON m.id = q.assistant_message_id
LEFT JOIN chat.action a ON a.message_id = m.id
WHERE q.id = $1;

-- name: ListByConversation :many
SELECT m.id, m.conversation_id, m.role, m.body, m.sources, m.created_at,
       coalesce(a.id, 0)::bigint AS proposal_id,
       coalesce(a.resource_id, '')::text AS resource_id,
       coalesce(a.resource_label, '')::text AS resource_label,
       coalesce(a.access_level, '')::text AS access_level,
       coalesce(a.task_id, '')::text AS task_id,
       coalesce(a.external_request_id, '')::text AS external_request_id,
       coalesce(a.status, '')::text AS status
FROM chat.message m
LEFT JOIN chat.action a ON a.message_id = m.id
WHERE m.conversation_id = sqlc.arg(conversation_id)
  AND (sqlc.arg(before_id)::bigint = 0 OR (m.created_at, m.id) < (
      SELECT created_at, id
      FROM chat.message
      WHERE conversation_id = sqlc.arg(conversation_id)
        AND id = sqlc.arg(before_id)::bigint
  ))
ORDER BY m.created_at DESC, m.id DESC
LIMIT sqlc.arg(page_limit)::bigint;
