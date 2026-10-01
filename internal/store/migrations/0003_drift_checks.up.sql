CREATE TABLE drift_checks (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    -- queued/running aren't produced by anything yet (phase 4 only runs
    -- checks synchronously, inserting one row once finished) but are
    -- included now since SQLite can't cheaply widen a CHECK constraint
    -- later, and the phase 5 scheduler will need them.
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'clean', 'drifted', 'failed')),
    started_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    duration_ms INTEGER,
    resources_added INTEGER NOT NULL DEFAULT 0,
    resources_changed INTEGER NOT NULL DEFAULT 0,
    resources_destroyed INTEGER NOT NULL DEFAULT 0,
    resources_unchanged INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    triggered_by TEXT NOT NULL CHECK (triggered_by IN ('manual', 'schedule', 'api'))
);

CREATE INDEX idx_drift_checks_workspace ON drift_checks (workspace_id, started_at DESC);

CREATE TABLE drift_resources (
    id TEXT PRIMARY KEY,
    drift_check_id TEXT NOT NULL REFERENCES drift_checks (id) ON DELETE CASCADE,
    resource_address TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    module_address TEXT,
    action TEXT NOT NULL CHECK (action IN ('no-op', 'create', 'update', 'delete', 'replace')),
    -- Already-redacted JSON (see internal/drift) - the only form of a
    -- resource's before/after values ever written here.
    before_json TEXT,
    after_json TEXT,
    has_sensitive BOOLEAN NOT NULL DEFAULT 0,
    has_unknown BOOLEAN NOT NULL DEFAULT 0
);

CREATE INDEX idx_drift_resources_check ON drift_resources (drift_check_id);
