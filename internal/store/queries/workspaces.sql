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

-- name: ListEnabledWorkspacesWithLastCheck :many
-- Deliberately does no date arithmetic here: it just joins each enabled
-- workspace to its last check's started_at (NULL if it's never been
-- checked) and leaves "is it actually due yet" - comparing that against
-- the workspace's own check_interval_minutes - to the scheduler, in Go,
-- where it's easy to test with a fake clock instead of relying on
-- SQLite's dynamic-interval date functions.
SELECT w.*, c.started_at AS last_check_started_at
FROM workspaces w
LEFT JOIN drift_checks c ON c.id = w.last_check_id
WHERE w.is_enabled = 1;
