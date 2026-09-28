# Linux installer hosting

`install.arveld.com` is a custom domain on the Cloudflare R2 bucket
`arveld-install`, in the same Cloudflare account as the `arveld.com` zone.
R2 serves these static files directly; no Worker is required. Binaries, images
and their release metadata remain on GitHub/GHCR.

## Published objects

| Path | Contents | Cache-Control |
| --- | --- | --- |
| `/VERSION/install-arveld.sh` | Controller installer for that exact release | `public, max-age=31536000, immutable` |
| `/VERSION/install-arveld-agent.sh` | Agent installer for that exact release | `public, max-age=31536000, immutable` |
| `/VERSION/SHA256SUMS` | Only the two installer checksums from the release | `public, max-age=31536000, immutable` |
| `/install-arveld.sh` | Most recently promoted stable controller installer | `no-store` |
| `/install-arveld-agent.sh` | Most recently promoted stable Agent installer | `no-store` |

`VERSION` is an actually published release tag. The root URLs are conveniences
and change only when a stable release is promoted. Prereleases publish only
their versioned paths. Every script embeds its own exact version; use versioned
paths when installing an Agent to match an
existing controller. There is no bucket listing or installer at `/`.

The root URLs become available with the first promoted stable release.
Prereleases must not create or replace them. Unversioned bootstrap scripts
must not be published to the root or to versioned paths.

## Cloudflare setup

Use Wrangler 4.142.0 and log in with an account that can manage R2 and read the
`arveld.com` zone. Set `CLOUDFLARE_ACCOUNT_ID` when using multiple accounts.
Create the bucket once, then attach the domain through R2 (which manages the
DNS record and TLS certificate):

```sh
bunx wrangler@4.142.0 r2 bucket create arveld-install --location weur
bunx wrangler@4.142.0 r2 bucket domain add arveld-install \
  --domain install.arveld.com --zone-id YOUR_ARVELD_ZONE_ID --min-tls 1.2
bunx wrangler@4.142.0 r2 bucket dev-url disable arveld-install
bunx wrangler@4.142.0 r2 bucket domain list arveld-install
```

Keep `r2.dev` disabled. Do not add a CNAME to an `r2.dev` URL. The custom domain
must become active before publication can pass its public download check.
Do not apply a cache rule that overrides the root scripts' `no-store` header.
Cloudflare's default cache eligibility is sufficient for correctness; caching
the versioned paths is optional. See [R2 public buckets](https://developers.cloudflare.com/r2/buckets/public-buckets/)
and the [Wrangler R2 commands](https://developers.cloudflare.com/workers/wrangler/commands/r2/).

## GitHub Actions configuration

Configure these Actions values on `RSTCK-Innovation/arveld`:

| Kind | Name | Value |
| --- | --- | --- |
| Variable | `CLOUDFLARE_ACCOUNT_ID` | Account owning the bucket and zone |
| Variable | `ARVELD_INSTALLERS_BUCKET` | `arveld-install` |
| Secret | `CLOUDFLARE_INSTALLERS_API_TOKEN` | Cloudflare API token with Account → Workers R2 Storage → Edit for that account |

Use a dedicated API token for publication. Do not copy a local Wrangler OAuth
session into GitHub Actions. The token is exposed only to the upload step;
CI and package builds have no R2 credentials. No DNS permission is needed for
ongoing object uploads.

## Publication and recovery

After GitHub publishes a release, the release workflow calls
[`installers.yml`](../../.github/workflows/installers.yml). It downloads only
the two installer assets and `SHA256SUMS` from that published immutable release;
it never rebuilds an installer from the current checkout. Promotion additionally
requires both a stable `vX.Y.Z` tag and GitHub's `prerelease` flag to be false.
Older releases that do not contain installers cannot be published this way.

[`publish-installers.sh`](../../scripts/publish-installers.sh) verifies both
scripts' checksums, syntax, version and component before any R2 write. It uploads
an explicit allowlist and a filtered checksum file, then updates the root scripts
only for an explicitly promoted stable version and after all versioned objects
have uploaded. The script rejects prerelease promotion before any R2 write,
including when invoked locally. Root updates are separate object writes; use
the versioned paths when both components must match. The workflow
downloads the versioned objects over HTTPS and compares them with the originals.

The R2 job is separate from GitHub release publication. A Cloudflare failure
leaves the immutable GitHub release intact. Recover only the installer hosting
job; do not rebuild or republish the release. Once this workflow is on `main`,
run:

```sh
gh workflow run installers.yml --ref main -f version=YOUR_PUBLISHED_RELEASE_TAG -f promote=false
```

The default recovery leaves the root URLs unchanged, so replaying an old release
does not move the entry points backwards. Use `-f promote=true` only when that
release is stable and should become the default download. The current `Prerelease`
workflow always sets `promote: false`; it does not publish stable releases.
Once an immutable stable GitHub release with installer assets is available,
promote it with the installer workflow, for example:

```sh
gh workflow run installers.yml --ref main -f version=v0.1.0 -f promote=true
```

Use an actually published stable version. Manual promotion of a prerelease is
rejected even if `promote=true` is supplied. All R2 publication jobs share one
concurrency group.

Public scripts still download binaries and their checksums from GitHub. While
the repository is private, use authenticated `gh release download` and
`ARVELD_RELEASE_DIR` as described in the [installation guide](../guides/installation.md).
No GitHub or Agent token is embedded in the public scripts.

Run the publication boundary tests with:

```sh
python3 -m unittest discover -s tests/installers -p 'test_*.py'
```
