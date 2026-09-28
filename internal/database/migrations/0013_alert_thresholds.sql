-- Preserve destination assignments while replacing the condition constraint.
CREATE TEMP TABLE saved_alert_destinations AS SELECT * FROM alert_rule_notifications;
DROP TABLE alert_rule_notifications;

CREATE TABLE alert_rules_next (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    monitor_id TEXT REFERENCES monitors (id) ON DELETE CASCADE,
    agent_instance_uid BLOB REFERENCES agents (instance_uid) ON DELETE CASCADE CHECK (agent_instance_uid IS NULL OR length(agent_instance_uid) = 16),
    condition TEXT NOT NULL CHECK (condition IN ('failed', 'no_data', 'latency', 'cpu', 'memory', 'disk')),
    threshold REAL,
    for_seconds INTEGER NOT NULL CHECK (for_seconds BETWEEN 1 AND 86400),
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    CHECK ((monitor_id IS NOT NULL AND agent_instance_uid IS NULL AND condition IN ('failed', 'no_data', 'latency'))
        OR (monitor_id IS NULL AND agent_instance_uid IS NOT NULL AND condition IN ('cpu', 'memory', 'disk'))),
    CHECK ((condition = 'latency' AND threshold IS NOT NULL AND threshold > 0 AND threshold <= 60000)
        OR (condition IN ('failed', 'no_data') AND threshold IS NULL)
        OR (condition IN ('cpu', 'memory', 'disk') AND threshold IS NOT NULL AND threshold > 0 AND threshold <= 100))
) STRICT;

INSERT INTO alert_rules_next (id, monitor_id, condition, for_seconds, severity)
SELECT id, monitor_id, condition, for_seconds, severity FROM alert_rules;
DROP TABLE alert_rules;
ALTER TABLE alert_rules_next RENAME TO alert_rules;
CREATE INDEX alert_rules_monitor_id ON alert_rules (monitor_id);
CREATE INDEX alert_rules_agent_id ON alert_rules (agent_instance_uid);

CREATE TABLE alert_rule_notifications (
    rule_id TEXT NOT NULL REFERENCES alert_rules (id) ON DELETE CASCADE,
    notification_id TEXT NOT NULL REFERENCES notification_channels (id) ON DELETE RESTRICT,
    PRIMARY KEY (rule_id, notification_id)
) STRICT;
CREATE INDEX alert_rule_notifications_notification_id ON alert_rule_notifications (notification_id);
INSERT INTO alert_rule_notifications SELECT * FROM saved_alert_destinations;
DROP TABLE saved_alert_destinations;
