# Back up and restore

Use a stopped-instance backup to capture SQLite, Prometheus and Alertmanager
together. Copying only `arveld.db` while Arveld is running can omit committed
changes in its WAL and does not preserve metrics or silences.

The commands below follow the [Linux service layout](https.md). Substitute your
actual data, configuration and executable paths.

## Make a backup

1. Record the controller revision or release, Agent image tags, configuration
   path and data directory. Keep the matching controller executable or a way to
   rebuild that exact revision.
2. Stop the controller and wait for it and both child engines to exit:

   ```sh
   sudo systemctl stop arveld
   sudo systemctl is-active arveld
   ```

   `inactive` is expected and returns a nonzero exit code. If shutdown fails,
   investigate before copying files. A foreground instance can be stopped with
   SIGTERM or Ctrl+C; wait for the process to exit.
3. Archive the entire data directory and the configuration. Choose a new backup
   filename and store it outside the data directory:

   ```sh
   sudo install -d -m 0700 /var/backups/arveld
   sudo sh -c 'umask 077; tar -czpf /var/backups/arveld/controller-YYYYMMDD-HHMMSS.tar.gz -C / var/lib/arveld etc/arveld'
   ```

   Replace the timestamp placeholder. Keep any SQLite sidecars present after
   shutdown; do not selectively delete them. Include deployment files and Agent
   credentials separately if they live outside these directories.
4. Restart the controller:

   ```sh
   sudo systemctl start arveld
   ```

   Sign in, open **Settings → Arveld health** and select **Refresh**. Confirm
   that all services are **Ready**.

Store a protected copy away from this host. Record the archive checksum and
verify it after transfer. A backup contains destination credentials and other
private configuration.

For the supplied Compose installation, use `docker compose stop controller`
and wait for shutdown before archiving the complete project-prefixed
`arveld-data` volume with a trusted local utility image. Include `compose.yaml`,
its environment overrides and the controller image ID or source revision.
Preserve UID/GID `10001:10001` when restoring into an empty volume. Restart with
`docker compose up --detach controller`; do not remove the original volume during backup.

## Back up Agent identity

For native execution, stop the Agent and archive its complete state directory:
`data/arveld-agent/` under its working directory, or the path selected by
`--storage-directory`. Record the matching executable version, connection
settings and file ownership. Restore to an empty directory with the original
Agent stopped, then start the matching executable with that directory.

Stop the Agent before archiving its state volume. For Docker, use a trusted
local utility image to mount `arveld-agent-data` read-only and archive its
contents, including hidden files. Record the volume name, image tag, controller
URL, host mounts and networking mode alongside the archive. Then restart the
Agent with the same volume.

For the repository's optional Compose Agent, use `docker compose stop agent`
and archive its project-prefixed `arveld-agent-data` volume. Restart with
`ARVELD_AGENT_TOKEN` restored in the environment and
`docker compose --profile agent up --detach agent`.

On restore, populate an empty volume before starting the replacement container.
Keep the original Agent stopped until you choose which copy will run.

## Restore to an isolated instance first

Use a fresh VM or container network namespace. A second HTTP port on the same
host is insufficient because managed engines use fixed loopback ports.

1. Keep the destination offline from Agents and notification recipients. Allow
   only your validation access. Restored rules and credentials can otherwise
   send real notifications or accept production Agents. Cache the managed
   executables before denying outbound network access if necessary.
2. Install the **same controller version** used for the backup. For this exercise,
   keep the same operating system and CPU architecture so the cached engine
   executables remain usable offline.
3. Verify the archive checksum and inspect its file listing. Extract into a new
   empty staging directory, not over a running instance:

   ```sh
   mkdir restored
   tar -tzf controller-YYYYMMDD-HHMMSS.tar.gz
   tar -xzpf controller-YYYYMMDD-HHMMSS.tar.gz -C restored
   ```

4. On the isolated host, place the restored data and configuration at the paths
   selected for that host. Set ownership to its service account. If the paths
   differ, update `database_path` in the restored YAML before starting.
5. Start Arveld and sign in with the restored administrator; do not create a
   new account. Open **Settings → Arveld health**, select **Refresh** and check
   that all services are **Ready**. Inspect the service logs if one is unavailable.
6. In **Agents**, check the saved identities; they should initially be
   disconnected in isolation. In **Monitors**, open a known service and compare
   its settings, **Monitoring rules** and historical measurements with the
   original instance. Check destinations in **Notifications**, persisted
   incidents in **Alerts** and active windows in **Maintenance**.
7. Restart the isolated instance again and repeat the reads. Keep the original
   archive unchanged until validation is complete.

Readiness alone is insufficient: a healthy empty instance is not a successful
restore. Compare specific records and measurement timestamps from before backup.

## Return to service

Stop the original controller before bringing the replacement onto the production
network. Move the reverse proxy to the replacement, restore Agent connectivity,
then verify fresh metrics and notification delivery with a target you control.
Retain the original data and backup for recovery.
