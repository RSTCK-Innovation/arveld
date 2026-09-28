# Run and configure the controller

The controller reads its settings from one YAML file. Use the
[installation guide](installation.md) for first startup and
[HTTPS setup](https.md) for a Linux service deployment.

## Run the controller

Start Arveld without arguments:

```sh
./arveld
```

Arveld reads all of its settings from one YAML file. When no `--config` option
is provided, it looks for `data/arveld.yml`. On the first run, if that file does
not exist, Arveld creates `data/` with private directory permissions and writes
the built-in defaults to `data/arveld.yml` with private file permissions, then
continues starting without requiring an edit or a second launch.

Use `--config` only to load a configuration file you have placed elsewhere:

```sh
./arveld --config /etc/arveld/arveld.yml
```

The web application is embedded; no separate frontend process is needed.
Keep secure session cookies and forward all paths from an HTTPS reverse proxy.

## Local HTTP development

For local plain-HTTP sessions, stop Arveld, change `session_cookie_secure` to
`false` in the generated `data/arveld.yml`, then start `./arveld` again. Keep the
default `true` for HTTPS deployments. This setting controls session cookies;
it does not enable TLS on the controller itself.

## Configuration settings

Agent keys are managed separately in SQLite. The controller configuration does
not contain or generate an agent credential.

The complete configuration is:

```yaml
http_address: 127.0.0.1:8080
session_cookie_secure: true
database_path: data/arveld.db
prometheus_retention_time: 15d
prometheus_retention_size: ""
prometheus_query_timeout: 30s
otlp_metrics_timeout: 30s
component_download_timeout: 2m
```

Arveld always installs and supervises its pinned Prometheus and Alertmanager
executables. There is no external-engine deployment mode.
`prometheus_retention_time` configures the maximum age of samples;
`prometheus_retention_size` optionally limits storage blocks with a value such
as `8GB`. When both are set, the first limit reached applies.

Arveld validates retention values before starting Prometheus. Durations
accept `y`, `w`, `d`, `h`, `m`, `s`, and `ms`, including combinations such as
`1w2d3h`. Sizes accept historical binary units (`B`, `KB`, `MB`, `GB`, `TB`,
`PB`, `EB`) or IEC binary units (`B`, `KiB`, `MiB`, `GiB`, `TiB`, `PiB`,
`EiB`). Decimal and combined values such as `1.5GiB` and `1GiB512MiB` are
accepted, but the two unit families cannot be mixed in one value. `0` disables
the corresponding policy, and an empty size disables only the size limit.

Open **Settings → General → Metrics retention** to
[read the active policy](../reference/metrics.md#read-active-retention).
This is read-only; edit this YAML file and restart Arveld to change retention.
The browser's workspace preferences do not configure the instance.

Fields omitted from an existing YAML file retain their built-in defaults.
Unknown fields are rejected so that a typo cannot silently disable a setting.
Existing configuration files are not rewritten automatically.
[`arveld.example.yml`](../../arveld.example.yml) illustrates common settings.
Use an absolute `database_path` for a service so changing the working directory
does not move its data; see [persistent data](persistent-data.md).

## Check service health

Open **Settings → Arveld health** and select **Refresh**. Each service should
show **Ready**; see the [startup walkthrough](installation.md#check-startup).
If one remains unavailable, inspect the controller logs before changing its
configuration.

Prometheus and Alertmanager listen only on the controller's loopback ports
`19090` and `19093`. Use Arveld to view measurements and configure notifications;
these internal ports do not need to be exposed.

## Reset the administrator password

If you cannot sign in, run the password-reset command on the controller host.
Use the installed controller executable and the existing configuration pointing
to your database. The command requires an existing administrator; it does not
create an account or a new database.

For the [Linux service layout](https.md):

1. Stop the controller:

   ```sh
   sudo systemctl stop arveld
   ```

2. Run the following command from an interactive terminal with Bash installed.
   Enter a new password of 15–128 characters at the hidden prompt. The password
   is passed through a pipe, without placing it in the command history:

   ```sh
   sudo -u arveld bash -c '
     set -eu
     IFS= read -r -s -p "New administrator password: " arveld_password
     printf "\n" >&2
     printf "%s\n" "$arveld_password" |
       /usr/local/bin/arveld reset-password \
         --config /etc/arveld/arveld.yml \
         --password-stdin
   '
   ```

   Wait for **Administrator password reset; existing sessions invalidated.**
   If the command fails, read its error and correct the password or paths before
   trying again. The CLI reads one password from a pipe or file; it does not
   accept direct terminal input or a password argument.

3. Restart the controller and sign in with the existing email address and the
   new password:

   ```sh
   sudo systemctl start arveld
   ```

The reset invalidates all existing browser sessions. It preserves the account's
email, Agent keys, API keys, Monitors and other configuration.

For a foreground installation, stop that process and adapt the executable and
configuration paths. Run the reset as the account that owns the database. When
`database_path` is relative, use the same working directory as normal startup.
Without `--config`, the reset command reads the existing `data/arveld.yml`.

## Network limits and component logs

The YAML configuration exposes three total operation limits, including reading
response bodies:

| Setting | Default | Operation |
| --- | --- | --- |
| `prometheus_query_timeout` | `30s` | PromQL queries and Prometheus management reads, including retention |
| `otlp_metrics_timeout` | `30s` | Each OTLP transfer to Prometheus |
| `component_download_timeout` | `2m` | Each managed component archive download |

Omitted settings retain these defaults, including in existing configuration
files. Newly generated configuration files include them. Use positive Go
durations such as `500ms`, `45s`, or `3m`; zero, negative values, and numbers
without units are rejected. Earlier caller cancellation still takes effect.
Restart Arveld after changing the configuration.

Managed Prometheus and Alertmanager emit JSON logs. Arveld preserves their
timestamp, severity, and message, adds `component` and `stream`, and places
additional upstream fields under `upstream`. The stream identifies where a log
was written; `stderr` does not imply an error. Non-JSON lines are retained at
`INFO`. Lines longer than 64 KiB are truncated and marked `truncated=true`.
