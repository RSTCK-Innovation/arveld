# HTTPS and service setup

Use an HTTPS reverse proxy in front of the controller. This example uses Caddy
and systemd on Linux, with the proxy and controller on the same machine. Adapt
the domain and filesystem paths to your host.

## Choose stable locations

| Path | Purpose |
| --- | --- |
| `/usr/local/bin/arveld` | Controller executable for this host |
| `/etc/arveld/arveld.yml` | Controller configuration |
| `/var/lib/arveld/` | SQLite, metrics and managed engine data |

Create a dedicated `arveld` system user. On a Debian-style system, for a fresh
installation with no existing account:

```sh
sudo useradd --system --home-dir /var/lib/arveld --shell /usr/sbin/nologin arveld
sudo install -d -o arveld -g arveld -m 0700 /etc/arveld /var/lib/arveld
sudo install -m 0755 ./arveld /usr/local/bin/arveld
```

Use an executable matching this host’s operating system and architecture. See
[installation and package availability](installation.md). Adapt the account command
for other distributions. Keep the configuration readable only by the service
account (owner `arveld:arveld`, mode `0600`).

Write `/etc/arveld/arveld.yml`:

```yaml
http_address: 127.0.0.1:8080
session_cookie_secure: true
database_path: /var/lib/arveld/arveld.db
prometheus_retention_time: 15d
prometheus_retention_size: ""
```

Absolute paths avoid changing the data location when the working directory
changes. See [all configuration settings](controller.md).

## Run as a service

Create `/etc/systemd/system/arveld.service`:

```ini
[Unit]
Description=Arveld uptime monitoring
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=arveld
Group=arveld
WorkingDirectory=/var/lib/arveld
ExecStart=/usr/local/bin/arveld --config /etc/arveld/arveld.yml
Restart=on-failure
RestartSec=5
TimeoutStopSec=90
UMask=0077

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now arveld
sudo systemctl status arveld
sudo journalctl -u arveld -f
```

Before exposing the instance, create the administrator through a local connection
or a temporarily access-restricted proxy. For remote setup, restrict the proxy
to your address until the account exists.

## Forward the entire origin

Install Caddy using its [official installation instructions](https://caddyserver.com/docs/install).
Point your chosen DNS name at the server and make ports 80 and 443 reachable for
Caddy's normal public certificate provisioning. Example Caddyfile:

```caddyfile
monitor.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy handles HTTPS and WebSocket upgrades. Forward **every path** so the web
application and Agent connections use the same origin.
Do not expose Prometheus or Alertmanager's loopback ports publicly. Use a
dedicated hostname at the origin root for the web application.

Validate and reload your Caddy configuration using your installation's service
commands. See Caddy's [reverse proxy guide](https://caddyserver.com/docs/quick-starts/reverse-proxy)
and [automatic HTTPS requirements](https://caddyserver.com/docs/automatic-https).

Open the HTTPS URL and sign in. In **Settings → Arveld health**, select
**Refresh** and confirm that every service is **Ready**. Open a Monitor or Agent
detail page and reload it directly to confirm that browser navigation works.

Use this HTTPS address as **Arveld URL** in the
[Agent installation wizard](advanced-installation.md#prepare-an-agent-installation). In
**Agents**, confirm that the Agent connects and sends fresh measurements through
the proxy.

Continue with [persistent data](persistent-data.md) and
[backup and restoration](backup-restore.md).
