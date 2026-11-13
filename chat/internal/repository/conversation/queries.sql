-- name: GetOrCreate :one
INSERT INTO chat.conversation (user_id)
VALUES ($1)
ON CONFLICT (user_id) WHERE is_default AND deleted_at IS NULL DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING id, user_id, created_at;
