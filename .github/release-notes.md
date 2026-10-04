This is Arveld's first public release candidate, with controller and Agent builds
for Linux amd64 and arm64. GitHub release downloads and GHCR images are public;
no GitHub account or registry login is required.

Every archive and runtime image includes a generated `THIRD_PARTY_NOTICES.txt`
containing the dependency license texts and notices. The Linux installers retain
these alongside Arveld's `LICENSE` and `NOTICE` in `/usr/local/share/licenses/COMPONENT/`.
GitHub build attestations cover the release files and both container image indexes;
see [provenance verification](https://arveld.com/docs/develop/releases/#provenance-and-visibility).

Download the installer scripts attached to this release, or use its versioned
URLs as described in [other installation methods](https://arveld.com/docs/operations/installation/).
The root convenience scripts referenced by Getting Started serve the latest
promoted stable release. Hosted versioned scripts become available after the
installer hosting job succeeds.

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

This is a prerelease: APIs, configuration and storage formats may change. Back up
the complete controller and Agent state before updating. There is no automatic
Agent executable upgrade or guaranteed downgrade path. See the
[update guide](https://arveld.com/docs/operations/updates/).
