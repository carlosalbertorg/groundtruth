CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    source_path TEXT NOT NULL,
    working_subdirectory TEXT,
    binary_kind TEXT NOT NULL CHECK (binary_kind IN ('terraform', 'tofu')),
    binary_version TEXT,
    credential_env_file TEXT,
    check_interval_minutes INTEGER NOT NULL DEFAULT 60 CHECK (check_interval_minutes > 0),
    check_timeout_seconds INTEGER NOT NULL DEFAULT 600 CHECK (check_timeout_seconds > 0),
    is_enabled BOOLEAN NOT NULL DEFAULT 1,
    -- Denormalized cache of the most recent check, for a fast workspace
    -- list without joining drift_checks on every request. Deliberately
    -- not foreign-keyed to drift_checks (added in a later migration):
    -- this is a cache, not a relationship that must hold referential
    -- integrity.
    last_check_id TEXT,
    last_check_status TEXT,
    created_by TEXT REFERENCES users (id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
