-- name: GetUserByEmail :one
SELECT id, email, password_hash, created_at
FROM auth.users
WHERE email = sqlc.arg(email);


-- name: CreateUser :one
INSERT INTO auth.users (email, password_hash)
VALUES (sqlc.arg(email), sqlc.arg(password_hash))
ON CONFLICT (email) DO NOTHING
RETURNING id, email, password_hash, created_at;

-- name: DeleteRefreshToken :exec
DELETE FROM auth.refresh_tokens
WHERE token_hash = sqlc.arg(token_hash);

-- name: CreateRefreshToken :exec
INSERT INTO auth.refresh_tokens (user_id, token_hash, expires_at)
VALUES (sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(expires_at));

-- name: RotateRefreshToken :execrows
UPDATE auth.refresh_tokens
SET token_hash = sqlc.arg(new_hash), expires_at = sqlc.arg(new_expires_at)
WHERE token_hash = sqlc.arg(old_hash)
  AND user_id = sqlc.arg(user_id)
  AND expires_at > now();
