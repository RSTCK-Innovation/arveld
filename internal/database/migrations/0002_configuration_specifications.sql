-- Nullable provenance preserves existing raw-YAML revisions and assignments.
ALTER TABLE agent_config_revisions ADD COLUMN specification TEXT
    CHECK (specification IS NULL OR json_valid(specification));

-- The last evaluated input is separate from immutable artifact provenance:
-- different inputs may produce identical bytes, or a target may fail to apply.
ALTER TABLE agent_config_assignments ADD COLUMN reconciled_specification TEXT
    CHECK (reconciled_specification IS NULL OR json_valid(reconciled_specification));
