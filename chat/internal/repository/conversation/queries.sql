-- name: GetOrCreate :one
INSERT INTO chat.conversation (user_id)
VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING id, user_id, created_at;
