# Install Arveld

The controller serves Arveld's web application, stores its configuration and
runs the metrics and notification engines. Agents connect to it to run Monitors
and report host measurements.

## Install a release candidate

Linux `amd64` and `arm64` candidates are available from
[GitHub Releases](https://github.com/RSTCK-Innovation/arveld/releases). The
repository and GHCR images are private: use an account with access. Candidates
are for evaluation; back up state before updates.

With GitHub CLI authenticated, download a specific version:

```sh
mkdir arveld-install && cd arveld-install
gh release download v0.1.0-rc.2 --repo RSTCK-Innovation/arveld \
  --pattern 'arveld-v0.1.0-rc.2_linux_amd64.tar.gz' \
  --pattern 'arveld-agent-v0.1.0-rc.2_linux_amd64.tar.gz' \
  --pattern SHA256SUMS --pattern compose.yaml
sha256sum --check --ignore-missing SHA256SUMS
```

Use `arm64` in both archive names for an ARM64 host. Run the checksum command
inside the download directory. It must succeed for every downloaded file.
`SHA256SUMS` detects file corruption; authenticate the download source as well.
The GitHub release is immutable after publication.

For Docker, authenticate with `docker login ghcr.io --username YOUR_GITHUB_LOGIN`
using a token with `read:packages` access, then run:

```sh
docker compose up --detach controller
```

The downloaded `compose.yaml` selects prebuilt images for that release, with no
source build. New releases use matching version tags for the controller and
Agent. Candidates through `v0.1.0-rc.2` use image digests. The release's
`images.txt` retains the digests for verification.
Use the same setup, volumes and optional Agent profile described below. Keep
this installation directory and Compose project name for future updates.

For native execution, extract the archive you need and check the version:

```sh
tar -xzf arveld-v0.1.0-rc.2_linux_amd64.tar.gz
./arveld --version
tar -xzf arveld-agent-v0.1.0-rc.2_linux_amd64.tar.gz
./arveld-agent --version
```

Continue with the native controller instructions below and the
[Agent guide](agent-setup.md). Runtime dependencies are included in the binaries;
the controller downloads the pinned metrics and notification engines at first
start. Their checksums are recorded in `components.lock.json` on the release.

## Install on Linux with the convenience script

Releases after `v0.1.0-rc.2` include `install-arveld.sh` and
`install-arveld-agent.sh`. Choose a published release containing these files;
the version below is an example. The scripts support Linux with systemd on
`x86_64` and `aarch64`: Ubuntu, Debian, AlmaLinux, Rocky Linux, Fedora, RHEL,
CentOS Stream, Oracle Linux, Amazon Linux, openSUSE/SLES and Arch Linux, including
derivatives declaring one of these families in `/etc/os-release`.

The two scripts are served by `install.arveld.com`, backed by Cloudflare R2.
Archives remain on GitHub Releases. Choose a published release containing the
installers, then download the versioned script:

```sh
VERSION=v0.1.0-rc.3
BASE="https://install.arveld.com/$VERSION"
curl -fL "$BASE/install-arveld.sh" -o install-arveld.sh
curl -fL "$BASE/SHA256SUMS" -o SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
sudo sh install-arveld.sh
```

The short URL `https://install.arveld.com/install-arveld.sh` serves the most
recently promoted stable installer. Each downloaded script still contains an exact
release version. Use the versioned URL above for reproducible installations.
The corresponding Agent script is `install-arveld-agent.sh` in either location;
always choose the version matching its controller.

During the transition, the root URLs retain their previously published
`v0.1.0-rc.3` scripts until the first stable release is promoted. New prereleases
never update the root URLs; use their exact versioned paths.

The installer detects the host architecture, installs missing prerequisites
using `apt-get`, `dnf`/`yum`, `zypper` or `pacman`, downloads that exact release's
archive and verifies its SHA-256 checksum before installing it. A running systemd
host and root privileges are required; use Docker for containers without systemd.
It does not select a moving `latest` version or upgrade the operating system.

Public access to the scripts does not grant access to private GitHub binaries.
While releases are private, or to prepare an offline binary installation,
download authenticated assets first. Replace `amd64` with `arm64` when needed:

```sh
gh release download "$VERSION" --repo RSTCK-Innovation/arveld \
  --pattern install-arveld.sh --pattern SHA256SUMS \
  --pattern "arveld-${VERSION}_linux_amd64.tar.gz"
sha256sum --check --ignore-missing SHA256SUMS
sudo env ARVELD_RELEASE_DIR="$PWD" sh install-arveld.sh
```

The system must still have the prerequisite packages. On first start, the
controller also needs network access to download its pinned managed engines.

The controller runs as the dedicated `arveld` account, with its executable at
`/usr/local/bin/arveld`, configuration at `/etc/arveld/arveld.yml`, and persistent
data in `/var/lib/arveld`. The installer enables and starts `arveld.service`.
It keeps the default listener at `127.0.0.1:8080` and secure cookies enabled:
complete the [HTTPS setup](https.md#forward-the-entire-origin), then create the
administrator. Inspect startup with `sudo journalctl -u arveld -f`.

Rerunning an installer replaces the executable and service definition, restarts
the service, and preserves existing configuration and data. Back up first when
changing versions. Put custom service settings in `systemctl edit arveld` so
they survive reinstalls. SELinux remains enabled; file labels are restored when
`restorecon` is available.

For the Agent script and its connection settings, see
[Connect an Agent](agent-setup.md#install-as-a-linux-service).

## Start locally with Docker Compose

From the repository root, with Docker and Compose 2.23.1 or later installed:

```sh
docker compose up --build --detach
docker compose logs --follow controller
```

The first start downloads the pinned Prometheus and Alertmanager executables.
Wait until the controller and both engines are ready, then open
[localhost:8080](http://localhost:8080) and create the administrator as described
below. To use another local port, run `ARVELD_HTTP_PORT=8081 docker compose up --detach`.

The supplied [Compose recipe](../../compose.yaml) publishes only
`127.0.0.1:8080`. It uses plain HTTP and disables secure-only cookies for this
local installation. For HTTPS behind a reverse proxy on the same host, start
with `ARVELD_SESSION_COOKIE_SECURE=true docker compose up --detach` and forward
all paths to the local port; see [HTTPS setup](https.md).

The controller runs as UID/GID `10001:10001`. Its named `arveld-data` volume holds
the database, metrics, notification state and downloaded engines under
`/var/lib/arveld`. Compose prefixes the volume name with the project name;
keep using the same project when recreating the service. The controller YAML
is supplied by `configs.controller-config.content` in `compose.yaml`.

Stop the installation with `docker compose down`; start it again with
`docker compose up --detach`. These commands preserve the data volume. Adding
`--volumes` to `down` deletes it and is not part of an update or normal shutdown.

After creating the administrator and an Agent key, you can activate the
[optional Agent service](agent-setup.md#use-the-local-compose-installation)
in this same recipe. Use `--profile agent` when starting or stopping both
services together; the default commands above manage only the controller.

## Run a native controller

### Prepare the controller host

Choose a machine with persistent storage and a network address your Agents can
reach. The controller executable includes the web application; Go, Bun and a
source checkout are not needed on the machine running it.

On first start, Arveld downloads its pinned Prometheus and Alertmanager
executables. Allow outbound HTTPS to the official release locations listed in
[the component manifest](../../internal/components/components.lock.json).
Metrics are retained for 15 days by default; allow disk space for your chosen
[retention policy](controller.md).

The Docker image uses this same controller and embedded frontend. Its default
configuration listens inside the container on `0.0.0.0:8080`, stores data under
`/var/lib/arveld` and keeps secure cookies enabled. The local Compose recipe
explicitly overrides that last setting for HTTP.

### Start Arveld

Place the controller executable built for your operating system and architecture
in a writable installation directory. From that directory, run:

```sh
./arveld
```

On the first run, Arveld creates `data/` and `data/arveld.yml` with its default
configuration, then continues starting. You do not need to create the directory
or configuration file yourself. Later runs reuse the existing configuration.

The default listener is `127.0.0.1:8080`, and the database is `data/arveld.db`.
Paths are relative to the working directory, so keep starting Arveld from this
directory. Edit the generated configuration only when you need different
settings; see [controller configuration](controller.md).

Secure session cookies are enabled by default for [HTTPS deployments](https.md).
For a local plain-HTTP session, follow the [local HTTP note](controller.md#local-http-development)
before signing in.

## Create the administrator

Open your Arveld HTTPS address, or [localhost:8080](http://127.0.0.1:8080) for
local HTTP development. Enter your name, email address and a password of 15–128
characters, then sign in. Arveld has one administrator
account. Complete this step before making the instance accessible to others.

The administrator manages Agents, Monitors, rules and notification channels.
Account and session details are in the [account and access guide](../reference/authentication.md).

## Check startup

1. Sign in and open **Settings → Arveld health**.
2. Select **Refresh**.
3. Check that all three services show **Ready** and the page reports
   **All services are ready.**

[![Arveld health showing the controller, metrics and notification services ready](../../website/public/product/instance-health.png)](../../website/public/product/instance-health.png)

*Product screenshot with example data. Select the image to enlarge it.*

The first start can take longer while the managed engines download. If a service
remains unavailable, inspect the controller's terminal output or service logs.
This screen checks the controller and its engines; confirm Agent connectivity
and notification delivery separately through their guides.

The engines use fixed loopback ports `19090` and `19093`. Docker keeps these
inside the controller container; they are not published. For another native
controller on the same machine, use a separate VM or container network namespace;
changing only its HTTP port is insufficient.

## Next steps

[Connect an Agent](agent-setup.md), then [create your first Monitor](first-monitor.md).
For a lasting installation, choose stable [data locations](persistent-data.md)
and set up [backups](backup-restore.md).
