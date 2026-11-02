This is Arveld's first stable release, with controller and Agent builds
for Linux amd64 and arm64. GitHub release downloads and GHCR images are public;
no GitHub account or registry login is required.

## What is included

- HTTP/HTTPS, TCP, ICMP and DNS monitoring from Agents on your infrastructure.
- Availability, response times and host metrics, with configuration and incident
  history stored locally.
- Alert rules, silences and notifications through email, Discord, Slack,
  Microsoft Teams, Telegram, PagerDuty and webhooks.
- An embedded web interface and managed Prometheus and Alertmanager processes.
- Updated and tested dependencies, including OpenTelemetry Collector core
  0.164.0 / contrib 0.164.1, Prometheus 3.15.1 and Alertmanager 0.34.2.

## Installation and verification

Every archive and runtime image includes a generated `THIRD_PARTY_NOTICES.txt`
containing the dependency license texts and notices. The Linux installers retain
these alongside Arveld's `LICENSE` and `NOTICE` in `/usr/local/share/licenses/COMPONENT/`.
GitHub build attestations cover the release files and both container image indexes;
see [provenance verification](https://arveld.com/docs/develop/releases/#provenance-and-visibility).

Download the installer scripts attached to this release, or use its versioned
URLs as described in [other installation methods](https://arveld.com/docs/operations/installation/).
The [Getting Started guide](https://arveld.com/docs/get-started/installation/)
uses the root convenience scripts, promoted to this stable version by the
installer hosting job. Hosted scripts become available after that job succeeds.

The release includes controller and Agent archives, multi-architecture container
images, standalone Linux installers and a Compose file with exact image tags.
`SHA256SUMS` records asset checksums; `images.txt` records image digests; SPDX files
inventory image dependencies. `components.lock.json` records the separately
downloaded Prometheus and Alertmanager executables and checksums. No moving
`latest` container tag is published.

The shared packaging workflow tests installation, login, Agent connectivity,
measurements and persistence on both native Linux architectures. It also checks
systemd installation and reinstallation. Distribution tests cover package
managers, permissions, preserved state and rejection of corrupt archives.

## Updating from a release candidate

Back up the complete controller and Agent state before updating, and update both
components to `v0.1.0`. There is no automatic
Agent executable upgrade or guaranteed downgrade path. See the
[update guide](https://arveld.com/docs/operations/updates/).
