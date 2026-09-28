-- Publication dates are immutable metadata. Older rows have no reliable date.
ALTER TABLE agent_config_revisions ADD COLUMN created_at_unix_ms INTEGER;
