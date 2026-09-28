CREATE TABLE notification_channels (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(trim(id)) > 0),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 2 AND 80),
    type TEXT NOT NULL CHECK (length(trim(type)) > 0),
    config TEXT NOT NULL CHECK (json_valid(config) AND json_type(config) = 'object')
) STRICT;
