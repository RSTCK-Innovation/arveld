# Agent development

The `agent/` directory owns Arveld's custom OpenTelemetry Collector, OpAMP
Supervisor integration and Docker build context. Its separate Go module keeps
the Agent's upstream dependency versions independent of the controller. These
commands build local artifacts; they do not download a published Arveld release.

## Build the standalone executable

From the repository root:

```sh
task agent:binary
```

This builds `bin/arveld-agent` for the current platform without cgo. All entry
points and implementation live under `agent/cmd/arveld-agent/`. One executable
contains the Supervisor and Collector; the Supervisor launches that same file
as a child process in Collector mode. No separate Collector executable, shell
script or Supervisor YAML is required for startup.

The executable reads `ARVELD_URL` and `ARVELD_AGENT_TOKEN`. Its default state
directory is `data/arveld-agent`, relative to the working directory; override
it with `--storage-directory=/absolute/path`. Preserve that directory to retain
the identity and local configuration. SIGINT and SIGTERM request graceful
Supervisor shutdown.

Managed host collection targets Linux. On a native Linux host the Collector
uses `/`; the Docker image sets `ARVELD_HOST_ROOT=/hostfs` for its read-only host
mount. Both modes execute the same program and use the same managed configuration.

`task agent:generate` regenerates only `agent/cmd/arveld-agent/components.go`
from `agent/collector-builder.yaml` using the pinned Collector Builder. Keep
generated files unchanged by hand. `task agent:test` and `task agent:test:race`
exercise the standalone executable; the root `task check` includes the Agent
module's formatting, ordinary tests, race tests and lint checks.

## Update Collector dependencies

Update the Collector and contrib modules as a compatible release set. Keep
`collector-builder.yaml`, the Collector Builder version in `Taskfile.yml`,
the provider module versions in `collector.go`, and local image references in
sync, then run `task agent:generate` to refresh the component inventory.

Collector contrib v0.161.0 requires `opamp-go` v0.23.0. Its Supervisor and
OpAMP extension do not compile against v0.24.0, which changes the package
manager interface and configuration protobuf types. The controller's separate
module can use v0.24.0; the Docker integration tests exercise their protocol
compatibility. Revisit the Agent constraint when upstream adopts the new API.

## Build the image

From the repository root:

```sh
task agent:build
```

[Taskfile.yml](../../Taskfile.yml) owns the local image tag, currently
`arveld-agent:0.161.0-arveld`.
[Dockerfile](../../agent/Dockerfile) compiles the same `cmd/arveld-agent` program
using the dependency versions in `agent/go.mod`. Its final image contains that
executable, HTTPS trust certificates and the writable state directory. It retains
UID/GID `10001:10001` for existing state volumes. Public Agent release packages
are not available yet.

To export the exact Linux executable used by the image, select the `binary`
build target. From the repository root:

```sh
docker build --platform linux/amd64 --target binary --output type=local,dest=bin/linux-amd64 agent
docker build --platform linux/arm64 --target binary --output type=local,dest=bin/linux-arm64 agent
```

Each output directory contains one `arveld-agent` executable. The runtime image
copies its executable from this same target; there is no separate Collector or
Supervisor executable to install.

Use `task package:linux` to build both controller and Agent images and export
both Linux architectures together; see [development setup](setup.md#package-linux-binaries-and-docker-images).

The image includes host metrics and HTTP/TCP/ICMP/DNS receivers, the resource
processor, OTLP HTTP exporter, OpAMP extension, diagnostic nop receiver/exporter
and file/env providers selected by [collector-builder.yaml](../../agent/collector-builder.yaml).
The binary's `collector components` output lists accepted component
names. The controller targets this distribution; other upstream receivers are
not implicitly included. A generic Contrib image cannot apply the current DNS
Monitor configuration.

## Connect to a local controller

For a controller running with the repository's Compose recipe, activate its
optional Agent service as described in the [installation guide](../guides/agent-setup.md#use-the-local-compose-installation).
It reaches the controller through the internal Compose network.

Start the controller using [development setup](setup.md), then create a key in
**Settings → Agent keys**. Docker must be able to reach the controller's listener.
For an isolated local development environment, set
`http_address: 0.0.0.0:8080` and restart the controller. Restrict access to your
development network, particularly while using plain HTTP.

```sh
export ARVELD_AGENT_TOKEN='<complete agent key>'
export ARVELD_URL='http://host.docker.internal:8080'
task agent:run
```

This task builds and starts the named `arveld-agent` container, adds the Docker
host mapping, passes the two variables, mounts `arveld-agent-data` at
`/var/lib/arveld-agent` and mounts the host root read-only at `/hostfs`.

```sh
docker stop arveld-agent
```

Stopping removes this development container because the task uses `--rm`; the
named state volume remains. Running the task again preserves its identity. Use
separate volumes for separate test Agents.

Use `task agent:run` above for a local development image. The installation wizard
uses the published Agent image matching the controller's release version and
does not accept a version or image override. See the
[operator guide](../guides/agent-setup.md#run-with-docker) for release installations.

## Startup and configuration

[supervisor.go](../../agent/cmd/arveld-agent/supervisor.go) accepts an HTTP(S) base URL with an
optional path prefix or trailing slash. It converts the scheme to WS(S) and
appends `/v1/opamp`. URLs containing credentials, whitespace, query strings or
fragments are rejected. Use `arveld-agent collector components` for the inventory
and `arveld-agent collector validate --config=/path/to/collector.yaml` for local
configuration diagnostics.

The same entrypoint configures persistent identity, local recovery and the
current executable as the managed Collector. The controller sends
configuration through OpAMP. Read the [configuration contract](agent-configuration.md)
before changing compilation, delivery or recovery. OpAMP does not upgrade the
Agent executable or verify its version before publishing configuration.

Host collection follows the [operator guide's deployment scope](../guides/agent-setup.md#state-and-host-collection).
The Docker image requires the `/hostfs` mount. Native Linux execution uses the
host root directly and needs no mount. An explicit `ARVELD_HOST_ROOT` overrides
that selection; the Agent resolves it locally, not the controller.
Docker Desktop measurements describe its Linux VM; network counters depend on
the Agent's network namespace.

## Verify a change

```sh
task test:agent
```

Run this after rebuilding the image. Docker-tagged checks exercise the pinned
Collector/Supervisor, compiled YAML, real protocol probes, configuration changes
and recovery. The configuration journey runs both mounted-host and native-root
modes in isolated Linux containers. It verifies host metrics received through
authenticated OTLP, HTTP execution, identity across restart and recovery after
a runtime configuration failure. These tests are separate from ordinary tests.

`TestAgentRequestsOpAMPWithEnvironment` observes a connection attempt to a local
fixture returning 503; it does not prove controller authentication or successful
configuration application. The [verification guide](verification.md#configuration-and-monitor-proofs)
identifies those journeys separately. A configuration validation result alone is
not evidence that a probe ran or that a notification was delivered.

The isolated Linux-container ICMP checks run without additional Docker
capabilities. Verify privileges on the target deployment platform rather than
assuming this is a cross-platform guarantee.
