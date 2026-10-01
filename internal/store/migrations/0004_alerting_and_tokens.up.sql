CREATE TABLE alert_destinations (
    id TEXT PRIMARY KEY,
    -- NULL means "applies to every workspace."
    workspace_id TEXT REFERENCES workspaces (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('generic_webhook', 'slack')),
    url TEXT NOT NULL,
    shared_secret TEXT,
    is_enabled BOOLEAN NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_alert_destinations_workspace ON alert_destinations (workspace_id);

CREATE TABLE alert_log (
    id TEXT PRIMARY KEY,
    destination_id TEXT NOT NULL REFERENCES alert_destinations (id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    drift_check_id TEXT REFERENCES drift_checks (id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (event_type IN ('drift_detected', 'resolved', 'check_failed')),
    success BOOLEAN NOT NULL,
    http_status INTEGER,
    response_snippet TEXT,
    attempted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    retry_count INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_alert_log_destination ON alert_log (destination_id, attempted_at DESC);
CREATE INDEX idx_alert_log_workspace ON alert_log (workspace_id, attempted_at DESC);

CREATE TABLE api_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    revoked_at TIMESTAMP
);

CREATE INDEX idx_api_tokens_user ON api_tokens (user_id);
