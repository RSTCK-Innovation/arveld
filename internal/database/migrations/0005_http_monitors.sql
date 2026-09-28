CREATE TABLE http_monitors (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    agent_instance_uid BLOB NOT NULL CHECK (length(agent_instance_uid) = 16),
    endpoint TEXT NOT NULL CHECK (length(trim(endpoint)) > 0),
    method TEXT NOT NULL CHECK (method IN ('GET', 'HEAD')),
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds BETWEEN 10 AND 3600),
    timeout_seconds INTEGER NOT NULL CHECK (
        timeout_seconds BETWEEN 1 AND 60 AND timeout_seconds <= interval_seconds
    ),
    FOREIGN KEY (agent_instance_uid) REFERENCES agents (instance_uid) ON DELETE RESTRICT
) STRICT;
