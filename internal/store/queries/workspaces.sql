-- name: CreateWorkspace :one
INSERT INTO workspaces (
    id, name, description, source_path, working_subdirectory,
    binary_kind, binary_version, credential_env_file,
    check_interval_minutes, check_timeout_seconds, is_enabled,
    created_by, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetWorkspace :one
SELECT * FROM workspaces WHERE id = ?;

-- name: GetWorkspaceByName :one
SELECT * FROM workspaces WHERE name = ?;

-- name: ListWorkspaces :many
SELECT * FROM workspaces ORDER BY name ASC;

-- name: UpdateWorkspace :one
UPDATE workspaces
SET
    name = ?,
    description = ?,
    source_path = ?,
    working_subdirectory = ?,
    binary_kind = ?,
    binary_version = ?,
    credential_env_file = ?,
    check_interval_minutes = ?,
    check_timeout_seconds = ?,
    is_enabled = ?,
    updated_at = ?
WHERE id = ?
RETURNING *;

-- name: DeleteWorkspace :exec
DELETE FROM workspaces WHERE id = ?;

-- name: UpdateWorkspaceLastCheck :exec
UPDATE workspaces SET last_check_id = ?, last_check_status = ? WHERE id = ?;
