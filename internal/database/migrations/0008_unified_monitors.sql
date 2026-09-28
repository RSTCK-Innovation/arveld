CREATE TABLE monitors (
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
    CHECK (
        (protocol = 'http' AND method IN ('GET', 'HEAD') AND ping_count = 0 AND dns_server = '' AND record_type = '' AND transport = '') OR
        (protocol = 'tcp' AND method = '' AND ping_count = 0 AND dns_server = '' AND record_type = '' AND transport = '') OR
        (protocol = 'icmp' AND method = '' AND ping_count BETWEEN 1 AND 10 AND ping_count <= timeout_seconds AND dns_server = '' AND record_type = '' AND transport = '') OR
        (protocol = 'dns' AND method = '' AND ping_count = 0 AND length(trim(dns_server)) > 0 AND record_type IN ('A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS') AND transport IN ('udp', 'tcp', 'tcp-tls'))
    ),
    FOREIGN KEY (agent_instance_uid) REFERENCES agents (instance_uid) ON DELETE RESTRICT
) STRICT;

CREATE INDEX monitors_agent_instance_uid_id ON monitors (agent_instance_uid, id);

-- Copy stored definitions verbatim; compilation history remains immutable.
INSERT INTO monitors (id, name, protocol, agent_instance_uid, endpoint, method, interval_seconds, timeout_seconds)
SELECT id, name, 'http', agent_instance_uid, endpoint, method, interval_seconds, timeout_seconds
FROM http_monitors;

INSERT INTO monitors (id, name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds, ping_count, dns_server, record_type, transport)
SELECT id, name, protocol, agent_instance_uid, endpoint, interval_seconds, timeout_seconds, ping_count, dns_server, record_type, transport
FROM network_monitors;

DROP TRIGGER network_monitor_id_is_unique;
DROP TRIGGER http_monitor_id_is_unique;
DROP TABLE network_monitors;
DROP TABLE http_monitors;
