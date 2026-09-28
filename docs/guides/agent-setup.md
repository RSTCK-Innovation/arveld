# Connect an Agent

An Agent runs Monitors from its own network and sends results and host metrics
to the controller. Install one where it can reach the services you want to
monitor: on a homelab host, inside a private network or on another site.

Start with a controller installed using the [latest stable convenience script](installation.md).
The controller and Agent must use the same release version.

## Create an Agent key

1. Open **Settings → Agent keys → Create key**.
2. Enter a **Key name** that identifies the deployment, then create the key.
3. Copy the complete value when it is shown; it cannot be retrieved afterwards.
   Keep it in your protected deployment environment or secret store.

Agent keys authorize connection and metrics ingestion. They do not grant access
to the management interface. Revoking a key disconnects Agents using it.

## Install as a Linux service

On the Agent host, download the latest stable convenience script and provide your
controller's HTTPS address and the key you just created:

```sh
curl -fsSL https://install.arveld.com/install-arveld-agent.sh -o install-arveld-agent.sh
export ARVELD_URL='https://monitor.example.com'
export ARVELD_AGENT_TOKEN='<complete agent key>'
sudo --preserve-env=ARVELD_URL,ARVELD_AGENT_TOKEN sh install-arveld-agent.sh
```

The address must be reachable from the Agent host. The script supports the same
[Linux distributions and architectures](installation.md#install-on-linux-with-the-convenience-script)
as the controller. It installs missing prerequisites, verifies the release
archive's checksum and version, then enables and starts `arveld-agent.service`
under the dedicated `arveld-agent` account.

If your controller is on another version, including an RC, use the
[Agent installation wizard](advanced-installation.md#prepare-an-agent-installation)
to obtain the script for that controller's exact version.

The executable is `/usr/local/bin/arveld-agent`; identity and local configuration
persist in `/var/lib/arveld-agent`. Connection settings, including the key, are
saved in `/etc/arveld-agent/agent.env`, owned by root with mode `0600`.
The service grants `CAP_NET_RAW` for ICMP Monitors without running as root.

Rerunning the installer without connection variables preserves its existing
settings. Providing both variables replaces the saved connection. In either
case, the Agent identity is preserved. Back up state before upgrades.

## Confirm the connection

1. Open **Agents** in Arveld.
2. Select the connected Agent and inspect its **Overview** for fresh host metrics.
3. Open **Configuration** and confirm that the requested revision is **Applied**.

Connection and configuration application are separate statuses; a connected
Agent can still report a configuration error.

If it does not connect, inspect the service:

```sh
sudo systemctl status arveld-agent
sudo journalctl -u arveld-agent -f
```

Check the controller URL, proxy access and key. If configuration application
fails, follow [configuration troubleshooting](../develop/diagnostics.md). Then
[create a Monitor](first-monitor.md) assigned to this Agent.

## State and host collection

Preserve `/var/lib/arveld-agent` when updating or reinstalling the service.
Never run two Agents from copies of the same identity. See
[persistent data](persistent-data.md) and [updates](updates.md).

Native Linux collection measures the host filesystem and interfaces directly.
Monitors run from that host's network environment. When a DNS result differs
from a lookup on another machine, compare it from the Agent host using the
[DNS troubleshooting guide](../reference/metrics.md#dns-results-that-differ-from-a-host-lookup).

For Docker, manual execution or an existing controller's exact version, see
[other installation methods](advanced-installation.md#prepare-an-agent-installation).
