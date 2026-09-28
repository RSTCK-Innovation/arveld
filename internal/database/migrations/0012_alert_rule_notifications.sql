CREATE TABLE alert_rule_notifications (
    rule_id TEXT NOT NULL REFERENCES alert_rules (id) ON DELETE CASCADE,
    notification_id TEXT NOT NULL REFERENCES notification_channels (id) ON DELETE RESTRICT,
    PRIMARY KEY (rule_id, notification_id)
) STRICT;

CREATE INDEX alert_rule_notifications_notification_id ON alert_rule_notifications (notification_id);
