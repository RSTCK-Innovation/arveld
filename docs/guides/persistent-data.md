# Persistent data

The directory containing `database_path` is also the controller's managed data
directory. With the default `database_path: data/arveld.db`, paths below are
relative to the directory from which you start Arveld, **not** the YAML file's
directory. Use absolute paths in a service deployment.

| Default location | Contents | Preserve when replacing the executable? |
| --- | --- | --- |
| `data/arveld.db` | Administrator, sessions and keys, Agent inventory and configurations, Monitors, rules, channels, incidents and acknowledgments | Yes |
| `data/arveld.db-wal`, `data/arveld.db-shm` | SQLite write-ahead log and shared-memory sidecars, when present | Keep with the database; never copy only a live `.db` file |
| `data/prometheus/` | Metric samples, Monitor results and time-series history | Yes |
| `data/alertmanager/` | Notification log and silence state | Yes |
| `data/config/` | Generated engine configuration and alert-rule files | Include in a full backup; Arveld owns and rewrites these |
| `data/components/<name>/<version>/` | Downloaded and verified engine executables and cache metadata | Retain to avoid downloading them again |
| `data/arveld.yml` by default | User-owned controller settings | Yes; back up separately if stored elsewhere |

Generated configuration is not a user configuration interface. Change Monitors,
rules and channels through Arveld; change instance settings in the controller
YAML. Direct edits to generated engine files will be replaced.

The supplied Docker Compose recipe mounts its named `arveld-data` volume at
`/var/lib/arveld` and sets `database_path` to `/var/lib/arveld/arveld.db`.
All controller stores and managed executables therefore share this volume.
Preserve the Compose project name and volume when replacing the container.
Keep `compose.yaml` and any environment overrides with the backup: its mounted
controller configuration is separate from the data volume.

## Agent state

Each Agent needs its own persistent state directory. Docker uses a volume
mounted at `/var/lib/arveld-agent`, named `arveld-agent-data` in the standard
launch. The native executable uses `data/arveld-agent/` relative to its working
directory, or the absolute path selected by `--storage-directory`. This state
holds the Supervisor's identity, configuration and recovery data. Preserve it
when replacing the executable or container.

The repository's optional Compose Agent uses a separate `arveld-agent-data`
volume prefixed by the same project name as the controller's volume. Keep both
volumes and that project name across installation updates.

Never use copies of the same identity directory in two running Agents. Keep
connection settings and the Agent key in your deployment's protected environment
file or secret store. A database backup contains key hashes, not a recoverable
copy of the complete Agent key shown at creation.

The read-only `/:/hostfs` mount is the monitored host filesystem, not Agent state.
On Docker Desktop the host measurements describe the Linux VM.

## Retention and access

Prometheus retains samples for 15 days by default. Configure time and optional
size limits in [the controller YAML](controller.md); restart to apply changes.
Settings displays the active retention policy. SQLite incident history is
separate from Prometheus sample retention.

Notification credentials and some Monitor request settings are stored in the
database and generated configuration. Protect both the data directory and its
backups. Browser workspace preferences remain in that browser's local storage;
they are not part of a controller backup.

Use [the complete backup procedure](backup-restore.md) to keep these stores
consistent.
