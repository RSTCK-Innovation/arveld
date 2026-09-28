-- An Agent report marks the selected target failed without changing intent.
ALTER TABLE agent_config_assignments ADD COLUMN failed_revision INTEGER
    CHECK (failed_revision IS NULL OR failed_revision = desired_revision);

CREATE TABLE agent_remote_config_working (
    instance_uid BLOB PRIMARY KEY NOT NULL,
    revision INTEGER NOT NULL,
    reported_at_unix_ms INTEGER NOT NULL,
    FOREIGN KEY (instance_uid, revision)
        REFERENCES agent_config_revisions (instance_uid, revision) ON DELETE CASCADE
) STRICT;

-- Only existing positive evidence can establish a working revision. Neither a
-- previous assignment nor an unknown reported hash proves successful application.
INSERT INTO agent_remote_config_working (instance_uid, revision, reported_at_unix_ms)
SELECT statuses.instance_uid, revisions.revision, statuses.reported_at_unix_ms
FROM agent_remote_config_statuses AS statuses
JOIN agent_config_revisions AS revisions
  ON revisions.instance_uid = statuses.instance_uid
 AND revisions.config_hash = statuses.config_hash
WHERE statuses.apply_status = 'applied';

UPDATE agent_config_assignments AS assignments
SET failed_revision = desired_revision
WHERE EXISTS (
    SELECT 1 FROM agent_remote_config_statuses AS statuses
    JOIN agent_config_revisions AS revisions
      ON revisions.instance_uid = statuses.instance_uid
     AND revisions.config_hash = statuses.config_hash
    WHERE statuses.instance_uid = assignments.instance_uid
      AND revisions.revision = assignments.desired_revision
      AND statuses.apply_status = 'failed'
);
