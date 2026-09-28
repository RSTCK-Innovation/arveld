This release candidate supports Linux amd64 and arm64.

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
