-- name: CreateAPIToken :one
INSERT INTO api_tokens (id, user_id, name, token_hash, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAPITokensForUser :many
SELECT * FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC;

-- name: GetActiveAPITokenByHash :one
SELECT * FROM api_tokens WHERE token_hash = ? AND revoked_at IS NULL;

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = ? WHERE id = ?;

-- name: RevokeAPIToken :exec
UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND user_id = ?;
