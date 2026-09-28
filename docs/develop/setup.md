# Development setup

This guide is for contributors and local validation of unreleased code.
Run commands from the repository root. Use [the codebase map](codebase.md)
to find the package responsible for a workflow.

## Development workflow

Keep changes focused on one behavior. For product code, write a focused test,
confirm the failure, implement the smallest coherent change, then run the
relevant wider checks. Keep refactors separate, preserve unrelated working-tree
changes and explain non-obvious choices in the change description.

Write code, tests and repository documentation in English. Customer-facing
terminology is **Agent** and **Monitor**. Update the relevant guide or reference
when behavior changes. Documentation and landing work uses builds, link checks
and browser review rather than a behavioral test suite.

## Requirements

- Go matching `go.mod`
- Bun 1.4.2 for frontend development, source builds and the complete local verification gate
- Task, for the repository's development commands
- curl and jq, for the component manifest helper tasks
- Docker with Compose 2.23.1 or later, for controller packaging and real Agent checks

## Build and start a local controller

```sh
task web:install
task build
task hooks:install
```

Run `task hooks:install` after every fresh clone. It configures this checkout to
use the versioned `.githooks/pre-commit` hook; Git does not copy that local setting
when cloning. Before each commit, the hook runs `task pre-commit` to check Go
formatting and lint in both modules and build the embedded frontend.

The result is `bin/arveld` for the current OS and architecture, with the frontend
embedded. Bun and the source checkout are build-time dependencies only.

Start it without arguments:

```sh
./bin/arveld
```

The first run creates `data/` and `data/arveld.yml` with the default settings,
then continues starting. Later runs reuse that file. No initial configuration
file or `--config` argument is needed.

Before signing in over local plain HTTP, follow the
[local HTTP note](../guides/controller.md#local-http-development), then restart
with `./bin/arveld`. Open [localhost:8080](http://127.0.0.1:8080) and create the
administrator. For remote access, use the [HTTPS deployment guide](../guides/https.md).

The first start downloads the pinned engines and uses loopback ports `19090`
and `19093`. Only one controller can use these ports in a network namespace.
Give isolated test instances separate VM or container network namespaces and
data directories.

`task run` rebuilds and starts the same controller. To use a different existing
configuration file, pass `CONFIG_FILE=/path/to/arveld.yml`.
For frontend hot reload, use [the frontend guide](frontend.md). To connect a
real Docker Agent, follow [Agent development](agent.md), including its controller
network-access requirements.

Go test/lint Task commands also build the assets first. Direct Go commands
require `task web:build` before compilation and after frontend changes.

## Package Linux binaries and Docker images

```sh
task package:linux
task test:controller:docker
```

`task package:linux` builds both local Docker images for the Docker host's
architecture, then exports `arveld` and `arveld-agent` into `bin/linux-amd64/`
and `bin/linux-arm64/`. Each runtime image copies its executable from the same
`binary` stage used by the native export. Docker supplies the build toolchains;
no local Go or Bun installation is needed for this packaging command.
To rebuild only one image, use `task controller:build` or `task agent:build`.

The root [Dockerfile](../../Dockerfile) builds the frontend with the pinned Bun
version, embeds it in the Go executable and copies that executable into the
runtime image. The image includes HTTPS trust certificates and a writable state
directory owned by UID/GID `10001:10001`. Native and Docker execution share the
same controller implementation. Build inputs exclude local state and dependencies.

The test starts the checked-in Compose recipe under a unique project name and
an automatically allocated local port. It downloads and starts the real pinned
engines, creates an administrator and Agent key, and connects the real Agent.
It receives host metrics and assigns an HTTP Monitor to that Agent. It then
removes and recreates both containers, verifies the session, key, Monitor and
historical measurement, and waits for a fresh probe using the original identity.
Only that test project's containers, network and volumes are removed afterward.

To export a standalone Linux controller from the same build output:

```sh
docker build --platform linux/amd64 --target binary --output type=local,dest=bin/linux-amd64 .
docker build --platform linux/arm64 --target binary --output type=local,dest=bin/linux-arm64 .
```

Each directory contains `arveld`, with the frontend embedded. The standalone
executable retains its native startup defaults; Docker passes the container
configuration through `--config`. See [other installation methods](../guides/advanced-installation.md)
for the local Compose workflow and its persistence rules.

## Validate the operator journey

On a fresh isolated instance, create the administrator, connect a real Agent,
create a Monitor for a controlled target, and assign a notification channel to
its rule. Make the target fail, confirm receipt at the destination, restore it
and verify the recovery result and notification. Restart the controller and
Agent, then check identities, configuration, history and fresh measurements.

Use [the verification guide](verification.md) for focused checks. Local builds,
fixtures and isolated engine tests each cover specific boundaries. They do not
replace running this journey with the actual release binaries and Docker images.

## Update the managed component manifest

[`internal/components/components.lock.json`](../../internal/components/components.lock.json)
pins the upstream executables that an Arveld release will manage. Each
component entry contains its exact version and, for every supported platform,
the official download URL, SHA-256 checksum, and archive size. Arveld embeds
this checked-in metadata instead of selecting the latest upstream release at
runtime.

Prometheus and Alertmanager use independent version numbers. They are
nevertheless updated together so that one Arveld revision always identifies a
tested pair of upstream components. The component manifest contains
macOS, Linux, and Windows entries on both `amd64` and `arm64`. These entries
describe upstream engine downloads, not a published Arveld release matrix. `amd64` is the name used
by Go and the upstream release index for the x86-64 architecture.

Generate the manifest by passing both versions explicitly:

```sh
task components:manifest:update \
  PROMETHEUS_VERSION=3.14.0 \
  ALERTMANAGER_VERSION=0.34.0
```

The task runs [`scripts/update-components-lock.sh`](../../scripts/update-components-lock.sh),
which reads the official Prometheus project release index at
`https://prometheus.io/download.json`. The script requires exactly one release
for each requested version and all six supported platform combinations. It
also rejects non-HTTPS URLs, malformed SHA-256 checksums, and non-positive
archive sizes.

The new manifest is first written beside the existing file under a temporary
name. It replaces `internal/components/components.lock.json` only after the
complete document has been generated successfully. Review and commit the
manifest diff together with the Arveld change that adopts the new component
versions.

For the complete gate and focused proof selection, use [the test guide](verification.md).
