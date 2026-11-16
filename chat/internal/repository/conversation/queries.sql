-- name: GetOrCreate :one
INSERT INTO chat.conversation (user_id)
VALUES ($1)
ON CONFLICT (user_id) WHERE is_default AND deleted_at IS NULL DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING id, user_id, created_at;

-- name: List :many
SELECT c.id,
       COALESCE((SELECT left(m.body, 80) FROM chat.message m
                 WHERE m.conversation_id = c.id AND m.role = 'user'
                 ORDER BY m.id LIMIT 1), c.title)::text AS title,
       c.created_at
FROM chat.conversation c
WHERE c.user_id = $1 AND c.deleted_at IS NULL
  AND (NOT c.is_default OR EXISTS (SELECT 1 FROM chat.message m WHERE m.conversation_id = c.id))
ORDER BY c.created_at DESC, c.id DESC;

-- name: Create :one
INSERT INTO chat.conversation (user_id, is_default)
VALUES ($1, false)
RETURNING id, title, created_at;

-- name: Delete :execrows
UPDATE chat.conversation SET deleted_at = now()
WHERE user_id = $1 AND id = $2 AND deleted_at IS NULL;

-- name: DeleteAll :exec
UPDATE chat.conversation SET deleted_at = now()
WHERE user_id = $1 AND deleted_at IS NULL;

-- name: GetByID :one
SELECT id, user_id, created_at FROM chat.conversation
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;
