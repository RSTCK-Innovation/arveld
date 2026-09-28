# Connect an Agent

An Agent runs Monitors from its own network and sends results and host metrics
to the controller. Install one where it can reach the services you want to
monitor: on a homelab host, inside a private network or on another site.

Download the Agent archive matching your controller version and Linux
architecture from the [release installation guide](installation.md#install-a-release-candidate).
For Docker, use the matching release tag, such as
`ghcr.io/rstck-innovation/arveld-agent:v0.1.0-rc.2`.
The release's `images.txt` also records the digest for verification or digest pinning.
Authenticate to GHCR on the Agent host while packages remain private.
Local builds are described in [the Agent development guide](../develop/agent.md).

## Create an Agent key

1. Open **Settings → Agent keys → Create key**.
2. Enter a **Key name** that identifies the deployment, then create the key.
3. Copy the complete value when it is shown; it cannot be retrieved afterwards.
   Keep it in your protected deployment environment or secret store.

Agent keys authorize connection and metrics ingestion. They do not grant access
to the management interface. Revoking a key disconnects Agents using it.

## Use the local Compose installation

If you started the controller with the repository's [Compose recipe](installation.md#start-locally-with-docker-compose),
or a release Compose file, run these commands from the same directory and Compose project:

```sh
export ARVELD_AGENT_TOKEN='<complete agent key>'
docker compose --profile agent up --detach
docker compose logs --follow agent
```

For the source recipe, add `--build` on first startup. The release recipe pulls
its prebuilt images.

The optional `agent` profile starts an Agent alongside the controller. It uses
`http://controller:8080` on their shared Docker network, reads the key from your
environment and stores its identity in the project-prefixed `arveld-agent-data`
volume. The key is not written into `compose.yaml`. Supply it again whenever
Compose creates or recreates the Agent container; keep a protected copy outside
the checkout.

Stop both services with `docker compose --profile agent down`. To restart them,
restore the key in your environment and run
`docker compose --profile agent up --detach`. Keep the same project name and
both volumes; do not add `--volumes` to a normal shutdown. This Agent monitors
the Docker host (Docker's Linux VM on Docker Desktop).

For an Agent on another machine, use the Docker or native instructions below.

## Prepare the installation

1. Open **Agents → Install agent**.
2. In **Environment**, choose **Linux convenience script** or **Docker run** as
   the **Installation method**, then select **Continue**. The Linux archive and
   Docker image always use the controller's release version; no version or
   image selection is needed. Automatic installation is unavailable when the
   controller reports a development or unknown version.
3. In **Connection**, enter the controller's reachable address in **Arveld URL**,
   such as `https://monitor.example.com`.
4. Select **Prepare installation**, then **Download installation script**.

[![Agent installation wizard showing the Linux script and release version](../../website/public/product/agent-install.png)](../../website/public/product/agent-install.png)

*Product screenshot with example data. Select the image to enlarge it.*

The address must be reachable from the Agent's Linux host or container. In a container,
`127.0.0.1` refers to that container, not the controller host. Use the same HTTPS
address that your proxy forwards to Arveld; see [HTTPS setup](https.md).

## Install as a Linux service

The Linux option downloads a self-contained installation script with your
controller's version and the URL you entered. It does not contain your Agent key.
On the target Linux host, run:

```sh
export ARVELD_AGENT_TOKEN='<complete agent key>'
sudo --preserve-env=ARVELD_AGENT_TOKEN sh install-arveld-agent-linux.sh
```

The installer supports the same [Linux distributions and architectures](installation.md#install-on-linux-with-the-convenience-script)
as the controller script. It installs missing prerequisites, verifies the
release archive's checksum and version, then enables and starts
`arveld-agent.service` under the dedicated `arveld-agent` account. The executable
is `/usr/local/bin/arveld-agent`; identity and local configuration persist in
`/var/lib/arveld-agent`. Connection settings, including the key, are saved in
`/etc/arveld-agent/agent.env`, owned by root with mode `0600`.
The service grants `CAP_NET_RAW` for ICMP Monitors without running as root.

Each new release also includes `install-arveld-agent.sh` for installation without
the wizard. Download it from `https://install.arveld.com/VERSION/install-arveld-agent.sh`,
using the controller's exact release tag in place of `VERSION`, and verify it
with that directory's `SHA256SUMS` as described for the controller. Then run:

```sh
export ARVELD_URL='https://monitor.example.com'
export ARVELD_AGENT_TOKEN='<complete agent key>'
sudo --preserve-env=ARVELD_URL,ARVELD_AGENT_TOKEN sh install-arveld-agent.sh
```

For private releases, download the script, `SHA256SUMS`, and the matching
`arveld-agent-VERSION_linux_ARCH.tar.gz` archive with authenticated `gh release
download`. Pass `ARVELD_RELEASE_DIR` to use those files locally:

```sh
sudo --preserve-env=ARVELD_URL,ARVELD_AGENT_TOKEN \
  env ARVELD_RELEASE_DIR="$PWD" sh install-arveld-agent.sh
```

Rerunning the release installer without connection variables preserves its
existing settings. Providing both variables replaces the saved connection.
In either case, the Agent identity is preserved. Back up state before upgrades.
Use `sudo systemctl status arveld-agent` and
`sudo journalctl -u arveld-agent -f` to inspect startup, then confirm the
connection and fresh measurements in Arveld.

## Run with Docker

The wizard prepares a script to run on the Agent host. It does not start a
container remotely. Authenticate to GHCR on that host before using a private
release image. The image tag matches the version of the Arveld controller.

Copy the downloaded script to the Agent host. In the terminal where you will run
it, provide the Agent key as instructed by the wizard:

```sh
export ARVELD_AGENT_TOKEN='<complete agent key>'
```

From the directory containing the downloaded file, run it with `sh`:

```sh
sh install-arveld-agent-docker.sh
```

The script uses the controller address you entered and its release version, and reads the key
from the terminal environment; the downloaded file does not contain the key.

The generated setup mounts a state volume at `/var/lib/arveld-agent` and the
host filesystem read-only at `/hostfs`. Preserve that state volume when replacing
the container. Use separate container names and state volumes if you adapt the
script to run several Agents on the same host.

## Run directly on Linux

Place the `arveld-agent` executable for your machine's architecture in a writable
installation directory. From that directory, provide the controller address and
the Agent key you created, then start it:

```sh
export ARVELD_URL='https://monitor.example.com'
export ARVELD_AGENT_TOKEN='<complete agent key>'
./arveld-agent
```

The executable contains both supervision and collection. It creates
`data/arveld-agent/` for its identity and local configuration. Keep starting it
from the same directory, or select a stable writable location with
`--storage-directory=/absolute/path`. No Docker mount or Supervisor YAML is
required. Ctrl+C or SIGTERM requests a graceful stop.

Native Linux collection measures the host filesystem and interfaces directly.
The account running the Agent needs access to the monitored resources; ICMP
permissions depend on the host's configuration.

## Confirm the connection

1. Return to Arveld and select **View agents**, or open **Agents** in the sidebar.
2. Select the connected Agent and inspect its **Overview** for fresh host metrics.
3. Open **Configuration** and confirm that the requested revision is **Applied**.

Connection and configuration application are separate statuses; a connected
Agent can still report a configuration error.

If it does not connect, inspect its terminal output, or its Docker logs:

```sh
docker logs --tail 100 arveld-agent
```

For the repository's Compose installation, use `docker compose logs --tail 100 agent`.

Check the controller URL, proxy access, key and host mount. If configuration
application fails, follow [configuration troubleshooting](../develop/diagnostics.md). Then
[create a Monitor](first-monitor.md) assigned to this Agent.

## State and host collection

Keep the state volume mounted at `/var/lib/arveld-agent` when updating or
recreating the container. For native execution, preserve the directory selected
by `--storage-directory`, or `data/arveld-agent/` by default. Never run two Agents
from copies of the same identity. See [persistent data](persistent-data.md) and
[updates](updates.md).

On a Linux Docker host, CPU, memory, filesystem and uptime measurements describe
the host. On Docker Desktop, they describe Docker's Linux VM. Host uptime is the
kernel's running time, not the age of the container.

Network counters describe interfaces visible to the Agent's network namespace.
The host-root mount alone does not select host interfaces. On Linux, adding
`--network host` (or `network_mode: host` in Compose) allows host-interface
collection. This also changes connectivity for every Monitor assigned to that
Agent. Docker Desktop does not expose macOS physical interfaces through the
Linux host metrics scraper.

Monitors reflect the Agent's network environment. When a DNS result differs from
a lookup on another machine, compare it from the Agent's environment using the
[DNS troubleshooting guide](../reference/metrics.md#dns-results-that-differ-from-a-host-lookup).
