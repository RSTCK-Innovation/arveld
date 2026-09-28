CREATE TABLE network_monitors (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    protocol TEXT NOT NULL CHECK (protocol IN ('tcp', 'icmp', 'dns')),
    agent_instance_uid BLOB NOT NULL CHECK (length(agent_instance_uid) = 16),
    endpoint TEXT NOT NULL CHECK (length(trim(endpoint)) > 0),
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds BETWEEN 10 AND 3600),
    timeout_seconds INTEGER NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 60 AND timeout_seconds <= interval_seconds),
    ping_count INTEGER NOT NULL DEFAULT 0,
    dns_server TEXT NOT NULL DEFAULT '',
    record_type TEXT NOT NULL DEFAULT '',
    transport TEXT NOT NULL DEFAULT '',
    CHECK (
        (protocol = 'tcp' AND ping_count = 0 AND dns_server = '' AND record_type = '' AND transport = '') OR
        (protocol = 'icmp' AND ping_count BETWEEN 1 AND 10 AND ping_count <= timeout_seconds AND dns_server = '' AND record_type = '' AND transport = '') OR
        (protocol = 'dns' AND ping_count = 0 AND length(trim(dns_server)) > 0 AND record_type IN ('A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS') AND transport IN ('udp', 'tcp', 'tcp-tls'))
    ),
    FOREIGN KEY (agent_instance_uid) REFERENCES agents (instance_uid) ON DELETE RESTRICT
) STRICT;

CREATE INDEX network_monitors_agent_instance_uid_id ON network_monitors (agent_instance_uid, id);

CREATE TRIGGER network_monitor_id_is_unique BEFORE INSERT ON network_monitors
WHEN EXISTS (SELECT 1 FROM http_monitors WHERE id = NEW.id)
BEGIN SELECT RAISE(ABORT, 'Monitor ID already exists'); END;

CREATE TRIGGER http_monitor_id_is_unique BEFORE INSERT ON http_monitors
WHEN EXISTS (SELECT 1 FROM network_monitors WHERE id = NEW.id)
BEGIN SELECT RAISE(ABORT, 'Monitor ID already exists'); END;
