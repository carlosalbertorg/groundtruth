-- name: CreateDriftResource :exec
INSERT INTO drift_resources (
    id, drift_check_id, resource_address, resource_type, module_address,
    action, before_json, after_json, has_sensitive, has_unknown
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListDriftResourcesForCheck :many
SELECT * FROM drift_resources WHERE drift_check_id = ? ORDER BY resource_address ASC;
