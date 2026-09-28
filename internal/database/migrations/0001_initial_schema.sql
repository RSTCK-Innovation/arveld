CREATE TABLE agents (
    instance_uid BLOB PRIMARY KEY NOT NULL,
    connected INTEGER NOT NULL DEFAULT 0 CHECK (connected IN (0, 1)),
    hostname TEXT,
    version TEXT,
    last_seen_at_unix_ms INTEGER
) STRICT;

CREATE TABLE agent_config_revisions (
    instance_uid BLOB NOT NULL,
    revision INTEGER NOT NULL CHECK (revision > 0),
    config_hash BLOB NOT NULL CHECK (length(config_hash) = 32),
    content BLOB NOT NULL,
    PRIMARY KEY (instance_uid, revision),
    FOREIGN KEY (instance_uid) REFERENCES agents (instance_uid) ON DELETE CASCADE
) STRICT;

CREATE UNIQUE INDEX one_config_hash_per_agent
ON agent_config_revisions (instance_uid, config_hash);

CREATE TABLE agent_config_assignments (
    instance_uid BLOB PRIMARY KEY NOT NULL,
    desired_revision INTEGER NOT NULL,
    previous_revision INTEGER,
    CHECK (
        previous_revision IS NULL
        OR previous_revision <> desired_revision
    ),
    FOREIGN KEY (instance_uid) REFERENCES agents (instance_uid) ON DELETE CASCADE,
    FOREIGN KEY (instance_uid, desired_revision)
        REFERENCES agent_config_revisions (instance_uid, revision),
    FOREIGN KEY (instance_uid, previous_revision)
        REFERENCES agent_config_revisions (instance_uid, revision)
) STRICT;

CREATE TABLE agent_remote_config_statuses (
    instance_uid BLOB PRIMARY KEY NOT NULL,
    config_hash BLOB NOT NULL,
    apply_status TEXT NOT NULL
        CHECK (apply_status IN ('applying', 'applied', 'failed')),
    error_message TEXT NOT NULL,
    reported_at_unix_ms INTEGER NOT NULL,
    FOREIGN KEY (instance_uid) REFERENCES agents (instance_uid) ON DELETE CASCADE
) STRICT;

CREATE TABLE agent_remote_config_failures (
    instance_uid BLOB NOT NULL,
    config_hash BLOB NOT NULL,
    error_message TEXT NOT NULL,
    failed_at_unix_ms INTEGER NOT NULL,
    PRIMARY KEY (instance_uid, config_hash),
    FOREIGN KEY (instance_uid) REFERENCES agents (instance_uid) ON DELETE CASCADE
) STRICT;

CREATE INDEX latest_agent_remote_config_failure
ON agent_remote_config_failures (instance_uid, failed_at_unix_ms DESC);

CREATE TABLE users (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;

CREATE TABLE sessions (
    token TEXT PRIMARY KEY NOT NULL,
    data BLOB NOT NULL,
    expiry_ns INTEGER NOT NULL
) STRICT;

CREATE TABLE api_keys (
    id TEXT PRIMARY KEY NOT NULL,
    administrator_id INTEGER NOT NULL DEFAULT 1 CHECK (administrator_id = 1),
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission IN ('read', 'write')),
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    created_at_ns INTEGER NOT NULL,
    expires_at_ns INTEGER CHECK (expires_at_ns > created_at_ns),
    revoked_at_ns INTEGER,
    FOREIGN KEY (administrator_id) REFERENCES users (id) ON DELETE CASCADE
) STRICT;

CREATE TABLE agent_keys (
    id TEXT PRIMARY KEY NOT NULL,
    administrator_id INTEGER NOT NULL DEFAULT 1 CHECK (administrator_id = 1),
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    created_at_ns INTEGER NOT NULL,
    revoked_at_ns INTEGER,
    FOREIGN KEY (administrator_id) REFERENCES users (id) ON DELETE CASCADE
) STRICT;
