ALTER TABLE notification_channels ADD COLUMN delivery TEXT NOT NULL
    DEFAULT '{"group_by":"rule","group_wait_seconds":5,"group_interval_seconds":30,"repeat_interval_seconds":14400}'
    CHECK (json_valid(delivery) AND json_type(delivery) = 'object');
