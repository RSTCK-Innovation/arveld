# Release candidates

The [prerelease workflow](../../.github/workflows/release.yml) publishes Linux
`amd64` and `arm64` controller/Agent archives, multi-architecture GHCR images,
image SPDX inventories, SHA-256 checksums, standalone Linux installers for the
controller and Agent, and a Compose file using release image tags.
Both executables report the same Arveld version with `--version`. The Agent's
Collector dependency version is tracked separately in `agent/go.mod`.

Archives include Arveld's `LICENSE` and `NOTICE`, plus a single generated
`THIRD_PARTY_NOTICES.txt` for the component's dependencies. Runtime images include
these files under `/usr/share/licenses/arveld/` or `/usr/share/licenses/arveld-agent/`
and declare `Apache-2.0` in their OCI license label. Binary export targets also
include all three files. The project license copies in `agent/` support its independent Docker build
context and must match the root files; packaging checks this before building.
The Linux installers preserve them under `/usr/local/share/licenses/COMPONENT/`
with mode `0644` and root ownership, including after reinstallation.

The [notice generator](../../agent/scripts/third-party-notices.sh) uses pinned
`go-licenses` v2.0.1 with the build's platform, cgo setting and Go tags. It combines
the original license texts, additional upstream notices, versioned source archive
links and Go's own license. The controller also includes Vite's generated licenses
for the dependencies bundled in its frontend, including fonts. Both components'
notices also retain the Mozilla certificate data license for the trust bundle
copied into runtime images. Generation fails
if a Go dependency's license cannot be identified or a required document is missing;
no list of dependency names needs manual maintenance. Third-party terms remain
independent of Arveld's license. Review new dependencies' terms when updating them;
automated collection is not a legal compatibility assessment.

The generator lives in the Agent build context so both Dockerfiles can use the
same script. Generated notices are build outputs, not tracked source files.
Package archives take their copy directly from the tested image. SPDX inventories
continue to provide the separate machine-readable dependency inventory.

## Build and verify

[The package workflow](../../.github/workflows/packages.yml) is shared by CI and
publication. On each native Linux architecture it builds runtime images, checks
version output, extracts their exact executables, creates archives, and runs the
real Compose installation/persistence test. No registry credentials are available
in that workflow. The publisher downloads only artifacts from its own run and
pushes those tested images, without rebuilding them.

`scripts/package-installers.sh` embeds the exact release version and component
in `install-arveld.sh` and `install-arveld-agent.sh`. Both use the shared
`scripts/install-linux.sh`, also embedded in the Agent installation wizard.
They are included in `SHA256SUMS` and file attestations. The installer checks
the archive checksum and binary version before installation. A release installer
always selects its own immutable release, including for later reinstalls.

After publication, a separate job copies only those two scripts and their
checksums to Cloudflare R2 at `install.arveld.com`. Binaries remain on GitHub.
Configure the bucket, custom domain and publication credentials using the
[installer hosting guide](installer-hosting.md) before publishing the next release.

To reproduce on a Linux host of the matching architecture, install the development
tools, Docker and Compose, run `task web:install`, then:

```sh
scripts/package-release.sh v0.0.0-rc.0 amd64 /tmp/arveld-release
```

Use `arm64` on an ARM64 Linux host. `docker/compose.release.yaml` is a template;
publication resolves only its two image placeholders. Users download the generated
`compose.yaml` containing the exact release tag for both images:
`ghcr.io/rstck-innovation/arveld:VERSION` and
`ghcr.io/rstck-innovation/arveld-agent:VERSION`, with `VERSION` replaced by
the published tag. The local example above uses the test-only `v0.0.0-rc.0` label.
`images.txt` records each image as `image:version@sha256:digest` for verification
or deployments that require digest pinning. Attestations also retain the image
digests. No moving `latest` container tag is published.

## Publish

1. Merge the release changes through a pull request and wait for the `CI` and frontend dependency
   audit push runs on that exact `main` commit to succeed. Update release notes in
   `.github/release-notes.md` before merging when preparing the next candidate.
2. Update local `main` and inspect the commit to release. Create an annotated,
   SSH-signed tag using a key in `.github/release-signers`:

   ```sh
   git switch main
   git pull --ff-only
   VERSION=YOUR_NEW_RELEASE_CANDIDATE_TAG
   git tag -s "$VERSION" -m "Arveld $VERSION"
   git verify-tag "$VERSION"
   git push origin "$VERSION"
   ```

3. The workflow requires `vX.Y.Z-rc.N`, verifies the tag signature, checks that
   the commit belongs to `main`, and requires successful main CI and frontend dependency audit
   runs for that exact commit. Only its publication job has contents/packages write access.
4. Publication creates a draft, uploads every asset, then publishes it as a
   prerelease. Repository settings make its assets and tag immutable. Verify
   the release page, both image platforms, checksums and a fresh installation.
5. The installer hosting job publishes versioned script URLs with promotion
   disabled. The root URLs used by the Getting Started guide are reserved for
   the latest promoted stable release. They become available with the first
   stable promotion. If the job fails, recover using the
   [installer hosting procedure](installer-hosting.md#publication-and-recovery);
   the GitHub release remains published and must not be recreated.
6. The website job publishes the landing page and documentation from the same
   release commit to `arveld.com`. It checks the deployed commit and public
   pages. See [website publication and recovery](documentation.md).

The publisher refuses an existing draft or published release before pushing any
image tags, so rerunning a published version cannot replace its registry tags.
Version tags cannot be updated or deleted through the normal repository rules.
Publish a new candidate to correct a published release. If a run fails before
publication, inspect its logs. A failed upload can leave a draft: delete only
that unpublished draft before rerunning the failed publication job. Rerun via
Actions; never recreate or move the tag. The workflow's manual trigger must also
be dispatched against the existing signed tag, not a branch.

## Provenance and visibility

GitHub immutable releases provide release integrity. Explicit GitHub build
attestations for files and images run when the repository is public; GitHub Team
does not include those artifact attestations for private repositories. Private
candidates therefore do not claim those build attestations. See
[GitHub's availability rules](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).

SPDX inventories describe dependencies identified inside the images. Prometheus
and Alertmanager are downloaded at runtime and are recorded separately in
`components.lock.json`. Frontend source dependencies remain recorded in the
versioned Bun lockfile; an image inventory is not a full vulnerability audit.

Keep GHCR packages private while the repository is private. A later repository
visibility change does not automatically publish its packages; use the separate
[public-opening procedure](repository-security.md#before-opening-the-repository).
