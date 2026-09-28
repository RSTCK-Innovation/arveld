Arveld v0.1.0-rc.4 is the fourth release candidate for private local evaluation on Linux amd64 and arm64.

Changes since v0.1.0-rc.3:

- Reserve the root installer URLs for stable releases. Prereleases publish their scripts and checksums under their versioned path only; the existing root scripts keep serving v0.1.0-rc.3 until the first stable release is promoted.
- Reject root promotion unless both the version tag and GitHub release metadata identify a stable release. Publication tests cover prerelease rejection, validated script uploads and recovery without replacing the root scripts.
- Explain workflow checks in English logs, expose the application checks as separate steps, and clearly label deliberately rejected installer inputs. Distribution fixtures identify v0.0.0-rc.0 as a test version; native package logs describe the real binary, Compose and systemd checks.

Prometheus remains pinned to 3.14.0 and Alertmanager to 0.34.0. The scheduled update report does not automatically install newer engines or frontend libraries.

The release includes controller/Agent archives, multi-architecture container images and standalone installation scripts. The packaging workflow verifies installation, login, Agent connectivity, measurements and persistence on both native Linux architectures, and exercises real systemd installation and reinstallation. Separate distribution checks cover package managers, permissions, saved state and rejection of corrupt archives.

Download the archive for your architecture, `SHA256SUMS` and the generated `compose.yaml`. The Compose file selects both images by the exact release tag. `images.txt` records their digests; SPDX files inventory the image dependencies. `components.lock.json` records the separately downloaded Prometheus and Alertmanager executables and checksums.

This is a prerelease: APIs, configuration and storage formats may change. Back up the complete controller and Agent state before updating. There is no automatic Agent executable upgrade or guaranteed downgrade path.

Repository and container access remain private. Authenticate to GitHub/GHCR with an account that has access. For the public Linux scripts, download the private archive and checksums with authenticated `gh release download`, then pass `ARVELD_RELEASE_DIR` to the installer. No `latest` image tag is published. See the installation and update guides in the tagged source.
