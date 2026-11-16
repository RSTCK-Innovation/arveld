Arveld v0.1.1 updates the monitoring components and includes fixes for two Astro
security advisories affecting the documentation site's dependencies.

## Changes since v0.1.0

- The controller now manages Prometheus 3.16.0 and Alertmanager 0.35.0. An
  upgrade test using data from the engines shipped with v0.1.0 verified
  that a historical Prometheus sample remained queryable and an active
  Alertmanager silence survived restart.
- The Agent uses OpenTelemetry Collector core 0.165.0 and contrib/Supervisor
  0.165.1. The controller and Agent use Collector modules 1.71.0 and OpAMP Go
  0.25.0. Integration tests exercised remote configuration, network probes,
  alert delivery and persistence across container recreation.
- Controller and Agent binaries are built with Go 1.27.4. The Linux installer
  test matrix now includes Fedora 45, openSUSE Leap 16.1, UBI 9.9 and UBI 10.3.
- The web interface and documentation dependencies have been refreshed,
  including i18next 27, React-i18next 18 and Astro 7.4.12.

## Installation and upgrade

Controller and Agent archives and container images are available for Linux
amd64 and arm64. Use the [installation guide](https://arveld.com/docs/get-started/installation/)
for a new deployment. For an existing installation, back up the controller and
Agent state, then update both components to v0.1.1 using the
[update guide](https://arveld.com/docs/operations/updates/).

The attached Compose file selects the v0.1.1 images. The release includes
checksums in `SHA256SUMS`, image digests in `images.txt` and SPDX inventories
for both architectures. The [verification guide](https://arveld.com/docs/develop/releases/#provenance-and-visibility)
covers the release files and container image attestations.

[Full changelog since v0.1.0](https://github.com/RSTCK-Innovation/arveld/compare/v0.1.0...v0.1.1)
