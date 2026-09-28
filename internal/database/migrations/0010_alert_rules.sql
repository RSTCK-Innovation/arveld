CREATE TABLE alert_rules (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    monitor_id TEXT NOT NULL,
    condition TEXT NOT NULL CHECK (condition IN ('failed', 'no_data')),
    for_seconds INTEGER NOT NULL CHECK (for_seconds BETWEEN 1 AND 86400),
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    FOREIGN KEY (monitor_id) REFERENCES monitors (id) ON DELETE CASCADE
) STRICT;

CREATE INDEX alert_rules_monitor_id ON alert_rules (monitor_id);
