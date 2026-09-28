-- Historical snapshots deliberately do not reference deletable rules/resources.
CREATE TABLE incidents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id TEXT NOT NULL,
    rule_revision TEXT NOT NULL,
    owner_kind TEXT NOT NULL CHECK (owner_kind IN ('monitor', 'agent')),
    owner_id TEXT NOT NULL,
    owner_name TEXT NOT NULL,
    condition TEXT NOT NULL,
    threshold REAL,
    for_seconds INTEGER NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    opened_at INTEGER NOT NULL,
    last_evaluated_at INTEGER NOT NULL,
    closed_at INTEGER,
    close_reason TEXT NOT NULL DEFAULT '',
    acknowledged_at INTEGER,
    acknowledged_by TEXT NOT NULL DEFAULT '',
    CHECK ((closed_at IS NULL AND close_reason = '') OR
        (closed_at IS NOT NULL AND close_reason IN ('condition_ended', 'rule_changed', 'rule_removed')))
) STRICT;
CREATE UNIQUE INDEX incidents_open_rule ON incidents (rule_id) WHERE closed_at IS NULL;
CREATE INDEX incidents_owner ON incidents (owner_kind, owner_id, id DESC);
CREATE INDEX incidents_rule_evaluation ON incidents (rule_id, last_evaluated_at);
