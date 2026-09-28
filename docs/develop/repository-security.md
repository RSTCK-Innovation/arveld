# Repository protection

The repository stays private during release-candidate validation. The baseline
is stored in `.github/rulesets/` and applied by
`scripts/configure-repository.sh` using a repository administrator's authenticated
GitHub CLI. The script does not change visibility, organization policies or paid
security subscriptions. Run it from the repository root after reviewing changes.

## Enforced baseline

- `main`: pull requests, signed commits, linear history, resolved conversations,
  and passing Application, Documentation, Secrets, Frontend dependency audit and both Linux packaging checks.
  Branches must be up to date before merge. Direct push, force push and deletion
  are blocked; the ruleset has no administrator bypass.
- Only squash merges are enabled. Merged branches are automatically deleted.
  `CODEOWNERS` routes reviews to the maintainer. There is no required external
  approval while the project has one maintainer; increase it when another trusted
  reviewer joins, and enable code-owner/last-push review then.
- `v*` tags: creation is limited to repository administrators; separate rules
  block updating and deleting tags for everyone. The release workflow also
  verifies an SSH signature against `.github/release-signers`.
- Releases are immutable after publication. The release Compose file references
  images by their release tag. The publisher refuses to republish an existing
  release; `images.txt` records each tag with its digest for verification or
  digest-pinned deployments. GHCR tags alone are not an immutability guarantee.
- Actions use an explicit action allowlist and require full commit SHA pins.
  Default tokens are read-only and cannot approve pull requests. Only the release
  publisher can write contents/packages. PR workflows never receive publish
  credentials and do not use `pull_request_target`.
- Dependabot alerts/security updates and twice-monthly version updates cover Go, npm,
  Docker and Actions. The hosted Bun updater cannot read lockfile version 2, so
  frontend version updates remain manual using Bun 1.4.2, with available versions
  reported by the scheduled dependency update workflow. The frontend audit
  workflow checks the complete Bun lockfile on PRs, main pushes and weekly,
  blocking high/critical advisories. Restore Bun Dependabot updates after its
  hosted updater supports this format; do not downgrade the lockfile to bypass
  this limitation. Gitleaks scans full history with redacted output. Its
  allowlist contains only two literal public test/example values, not directories.

GitHub Team supports these private-repository rulesets. CodeQL and explicit build
attestation workflows are prepared but gated on public visibility. Their private
use would require additional GitHub products; the baseline does not enable them.

## Dependency update cadence and coverage

Dependabot checks GitHub Actions, the controller Go module, the Agent Go module,
the documentation website npm dependencies, and Docker base images on the **1st
and 16th of each month**. Checks are staggered between 07:17 and 07:57 in
`Europe/Paris`. This is a twice-monthly schedule, not an exact rolling 15-day
interval. It uses GitHub's supported `interval: cron` / `cronjob` configuration.
Dependency security alerts and security updates operate separately from that
version-update schedule; the weekly frontend security audit remains enabled.

The controller and Agent have separate Go update entries because their OpAMP
APIs have different compatibility requirements. Minor/major version updates of
`opamp-go` in the Agent are held for review with the upstream Supervisor; patch
updates remain eligible. Revisit that constraint when the Supervisor and OpAMP
extension adopt the newer API. Collector updates also require synchronizing the
Agent's builder manifest, generated inventory and local image references.

The **Dependency update report** workflow checks the unsupported sources on the
same calendar days at 06:43 UTC, on PRs changing its inputs, or on demand through Actions:

- Prometheus and Alertmanager: compare the pinned engines against stable releases
  from the official Prometheus download index, using numeric version ordering.
- Frontend libraries: run `bun outdated` with the project's pinned Bun version
  against the current lockfile, including development dependencies.

The report appears in the workflow run summary and in a
`dependency-update-report` artifact retained for 45 days. These checks only
report available updates: they do not open PRs, install new engines, or change
lockfiles. A successful run can therefore report pending updates. Maintainers
review this report alongside the Dependabot PRs, update engines together with
`task components:manifest:update`, and update frontend libraries using the
pinned Bun version. All resulting changes go through normal PR checks. Restore
native Bun Dependabot updates once its hosted updater accepts lockfile v2.

Run `scripts/check-component-updates.sh` locally to check engine releases.

## Before opening the repository

Opening is a separate maintainer decision. Do not infer authorization to change
visibility from a release task.

1. Arveld uses [Apache License 2.0](../../LICENSE), with contribution terms in
   [CONTRIBUTING.md](../../CONTRIBUTING.md). Review licenses and notices for all
   redistributed dependencies/assets; the project license does not replace their
   terms. Review the full Git history, LFS objects, issues, PRs,
   Actions logs/artifacts and screenshots for confidential material. A successful
   secret scan cannot establish that all this content is suitable for publication.
2. Recheck repository/organization access, administrator 2FA, signing keys,
   installed GitHub Apps, deploy keys, Actions secrets and package permissions.
   Remove obsolete access through the owning account. Confirm repository rules
   still apply without administrator bypass.
3. After the explicitly approved public visibility change, enable free public
   secret scanning and push protection, private vulnerability reporting, and
   require approval for **all outside collaborators** running fork PR workflows.
   Keep fork tokens read-only and never send secrets to forks. These public-only
   controls are unavailable on this private repository with its current plan.
4. Run CodeQL manually for Go and JavaScript/TypeScript, inspect the results, and
   add successful analysis checks/code-scanning merge protection. The Go job
   builds both Go modules. Review dependency alerts before public distribution.
   Keep the weekly CodeQL run enabled. Consider dependency-review gating once the
   dependency graph has been verified for both Go modules and frontend lockfiles.
5. Review the GHCR packages and explicitly change their visibility only when
   public distribution is approved. Verify an anonymous pull of both architectures
   and download/install each release archive without repository credentials.
6. Publish a new candidate from validated `main` to exercise public build
   attestations. Verify the release/asset integrity and image attestations with
   GitHub CLI; previously published private candidates are not retroactively
   attested by the workflow.

Reference: [Dependabot scheduling](https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference#schedule),
[ruleset availability](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets),
[secure Actions use](https://docs.github.com/en/actions/reference/security/secure-use),
[immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases),
[security settings](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-security-and-analysis-settings-for-your-repository).
