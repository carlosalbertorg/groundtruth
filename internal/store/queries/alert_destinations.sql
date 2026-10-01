-- name: CreateAlertDestination :one
INSERT INTO alert_destinations (
    id, workspace_id, name, kind, url, shared_secret, is_enabled, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAlertDestinations :many
SELECT * FROM alert_destinations ORDER BY created_at ASC;

-- name: ListAlertDestinationsForWorkspace :many
SELECT * FROM alert_destinations
WHERE is_enabled = 1 AND (workspace_id = ? OR workspace_id IS NULL);

-- name: GetAlertDestination :one
SELECT * FROM alert_destinations WHERE id = ?;

-- name: UpdateAlertDestination :one
UPDATE alert_destinations
SET
    workspace_id = ?,
    name = ?,
    kind = ?,
    url = ?,
    shared_secret = ?,
    is_enabled = ?
WHERE id = ?
RETURNING *;

-- name: DeleteAlertDestination :exec
DELETE FROM alert_destinations WHERE id = ?;
