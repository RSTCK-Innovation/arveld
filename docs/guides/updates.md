# Update the controller and Agents

An update replaces executables and containers while preserving persistent data.
It is not a reinstall. Read the target version's configuration and migration
notes before replacing a working instance. See [installation](installation.md)
for current package availability.

## Release Compose installation

Download the target release's `compose.yaml` and `SHA256SUMS` into a separate
directory and verify its checksum as in [release asset downloads](advanced-installation.md#download-release-assets). Back up
the complete current state and retain the previous Compose file. Replace only
`compose.yaml` in the existing installation directory; preserve `.env`, the
Compose project name and volumes. Restore `ARVELD_AGENT_TOKEN` if using the Agent.

```sh
docker compose --profile agent pull
docker compose --profile agent up --detach
```

Omit `--profile agent` when you only run the controller. Both images use the
target release's version tag, so replacing the Compose file selects that release.
Never use `down --volumes` for an update. Perform the readiness and continuity checks below.

## Controller

1. Obtain the replacement controller executable for your operating system and
   architecture. Verify it using the checksums supplied with that version.
   Prepare it as `/usr/local/bin/arveld.next` for the example below.
2. Record the running version and make a [complete backup](backup-restore.md).
3. Stop the service and retain the old executable:

   ```sh
   sudo systemctl stop arveld
   sudo cp -p /usr/local/bin/arveld /usr/local/bin/arveld.previous
   sudo install -m 0755 /usr/local/bin/arveld.next /usr/local/bin/arveld
   sudo systemctl start arveld
   ```

   These commands assume the [Linux service layout](https.md). Do not overwrite
   an older recovery executable without retaining the version you need.
4. Sign in and open **Settings → Arveld health → Refresh**. Confirm that all
   services are **Ready**. Inspect **Monitors**, their historical metrics and
   **Maintenance** windows. In **Agents**, confirm the existing Agents reconnect
   and produce fresh results. Inspect service logs if a check fails.

Keep the same YAML path, `database_path`, working directory and service account.
Arveld runs SQLite migrations on startup. Its embedded manifest selects the
Prometheus/Alertmanager versions, downloading a new pair when necessary.
Preserve the entire data directory, including both engines' state.

For the local Compose installation, back up first and retain the current image
for recovery. Build the selected source revision with `docker compose build
controller`, then run `docker compose up --detach controller`. Keep the same
Compose project name, configuration and named volume. Do not run `down --volumes`
as part of an update. Verify readiness and historical measurements afterward.

## Agent

Obtain the Agent executable or image matching the target Arveld version.
Update one Agent and verify it before replacing the others.

For native Linux execution, stop the Agent, retain its old executable, then
replace it with the new executable for the same architecture. Restart with the
same connection variables, working directory and `--storage-directory` setting.
Keep the whole state directory and its ownership intact.

For Docker, select the exact version tag. Stop and recreate the container with
the same:

- named state volume mounted at `/var/lib/arveld-agent`;
- `ARVELD_URL` and complete `ARVELD_AGENT_TOKEN`;
- host-root mount and chosen networking mode;
- restart policy and other deployment settings.

For Compose, update the image in your existing deployment file and recreate the
service without deleting its volume. Do not use `down --volumes` for an update.
If the container was launched with `--rm`, stopping removes the container but
retains its named volume; run the replacement with that same volume.

For the repository's local Compose recipe, restore `ARVELD_AGENT_TOKEN` in your
environment, then run `docker compose --profile agent up --build --detach`
from the selected source revision to rebuild and recreate both services. Retain
the previous images for recovery and keep the same project name and volumes.

Check that the existing Agent identity returns as connected, its configuration
is applied and its Monitors resume. A successful controller write does not prove
an older Agent can apply the new configuration. OpAMP distributes configuration;
it does **not** upgrade Agent executables.

## Recovery after a failed update

Stop the replacement before recovery. Restore the previous controller together
with its **pre-update data backup** into a clean destination. Do not assume an
older binary can read a newly migrated database or newer engine files.

For an Agent rollback, preserve the current state for diagnosis and restore the
matching pre-update state directory and executable or image if the new Supervisor
changed its state format. Start only one Agent using that identity.

Verify readiness, login, historical state and fresh measurements after recovery.
