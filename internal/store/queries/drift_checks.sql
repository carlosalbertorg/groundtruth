-- name: CreateDriftCheck :one
INSERT INTO drift_checks (
    id, workspace_id, status, started_at, finished_at, duration_ms,
    resources_added, resources_changed, resources_destroyed, resources_unchanged,
    error_message, triggered_by
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetDriftCheck :one
SELECT * FROM drift_checks WHERE id = ?;

-- name: ListDriftChecksForWorkspace :many
SELECT * FROM drift_checks WHERE workspace_id = ? ORDER BY started_at DESC LIMIT ?;
