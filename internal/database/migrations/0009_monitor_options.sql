CREATE TABLE monitors_next (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    protocol TEXT NOT NULL CHECK (protocol IN ('http', 'tcp', 'icmp', 'dns')),
    agent_instance_uid BLOB NOT NULL CHECK (length(agent_instance_uid) = 16),
    endpoint TEXT NOT NULL CHECK (length(trim(endpoint)) > 0),
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds BETWEEN 10 AND 3600),
    timeout_seconds INTEGER NOT NULL CHECK (timeout_seconds BETWEEN 1 AND 60 AND timeout_seconds <= interval_seconds),
    method TEXT NOT NULL DEFAULT '',
    ping_count INTEGER NOT NULL DEFAULT 0,
    dns_server TEXT NOT NULL DEFAULT '',
    record_type TEXT NOT NULL DEFAULT '',
    transport TEXT NOT NULL DEFAULT '',
    options TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(options) AND json_type(options) = 'object' AND (protocol = 'http' OR options = '{}')),
    skip_tls_verify INTEGER NOT NULL DEFAULT 0 CHECK (skip_tls_verify IN (0, 1) AND (skip_tls_verify = 0 OR protocol = 'http')),
    CHECK (
        (protocol = 'http' AND method IN ('GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS') AND ping_count = 0 AND dns_server = '' AND record_type = '' AND transport = '') OR
        (protocol = 'tcp' AND method = '' AND ping_count = 0 AND dns_server = '' AND record_type = '' AND transport = '') OR
        (protocol = 'icmp' AND method = '' AND ping_count BETWEEN 1 AND 10 AND ping_count <= timeout_seconds AND dns_server = '' AND record_type = '' AND transport = '') OR
        (protocol = 'dns' AND method = '' AND ping_count = 0 AND length(trim(dns_server)) > 0 AND record_type IN ('A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS') AND transport IN ('udp', 'tcp', 'tcp-tls'))
    ),
    FOREIGN KEY (agent_instance_uid) REFERENCES agents (instance_uid) ON DELETE RESTRICT
) STRICT;

-- New options default to legacy behavior; immutable configuration history is retained.
INSERT INTO monitors_next (id, name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds, method, ping_count, dns_server, record_type, transport)
SELECT id, name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds, method, ping_count, dns_server, record_type, transport FROM monitors;
DROP TABLE monitors;
ALTER TABLE monitors_next RENAME TO monitors;
CREATE INDEX monitors_agent_instance_uid_id ON monitors (agent_instance_uid, id);
