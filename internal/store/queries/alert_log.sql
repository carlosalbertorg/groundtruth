-- name: CreateAlertLogEntry :exec
INSERT INTO alert_log (
    id, destination_id, workspace_id, drift_check_id, event_type,
    success, http_status, response_snippet, attempted_at, retry_count
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListAlertLogForWorkspace :many
SELECT * FROM alert_log WHERE workspace_id = ? ORDER BY attempted_at DESC LIMIT ?;
