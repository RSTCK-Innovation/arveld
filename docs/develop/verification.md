# Verification

Choose checks for the boundary a change affects. Ordinary integration tests,
real engine processes, a real Agent and manual browser checks provide different
kinds of evidence. This guide describes how to run them; it is not a record of
past runs.

## Commands

Run from the repository root. [Taskfile.yml](../../Taskfile.yml) owns the commands;
[web/package.json](../../web/package.json) owns the frontend gate.

| Command | Scope |
| --- | --- |
| `task test:modules` | Module and CLI contracts |
| `task test:integration` | Real stores, application journeys and CLI executable |
| `task test` | Both levels, without cached results |
| `task test:race` | Both levels with the race detector |
| `task web:check` | Biome, Bun tests, TypeScript and production frontend build |
| `task check` | Formatting, configuration checks, ordinary/race Go tests, lint and frontend gate |
| `task agent:build` then `task test:agent` | Docker-tagged Collector/Supervisor executable journeys |
| `task package:linux` then `task test:controller:docker` | Controller and Agent packaging, real Compose measurements and persistence after recreation |
| `task test:installers DISTRO=almalinux:9` | Install/reinstall both services in a disposable distribution container; verify state, permissions and checksum rejection |

Install dependencies with `task web:install`. Go build/test/lint Task commands
build the embedded frontend first. For direct Go commands, run `task web:build`
before compilation and after frontend changes. Production build/test/lint tasks
disable cgo; the race gate enables it.

## Continuous integration

The [CI workflow](../../.github/workflows/ci.yml) runs on pull requests targeting
`main`, pushes to `main`, and manual dispatch. Application and documentation run on Ubuntu 24.04 `amd64`; packaging also
runs on a native Ubuntu 24.04 `arm64` runner:

| Check | Commands and coverage |
| --- | --- |
| Application | `task web:install`, then separate steps for `task lint:config`, `task fmt:check`, `task test`, `task test:race`, `task lint` and `task web:check` (the same checks as `task check`) |
| Documentation | `task website:install` then `task website:build`: locked dependencies and the production website build |
| Packages / Linux (amd64), Packages / Linux (arm64) | Release archives and images, version output, real Compose persistence, systemd installation and Agent reconnection on both native architectures |
| Secrets | Actionlint workflow validation and Gitleaks full-history scanning, with redacted output |
| Frontend dependency audit | `bun audit --audit-level=high`: all packages in the Bun lockfile, also checked weekly |
| Linux installers | Actual package managers, accounts and files on Ubuntu, Debian, AlmaLinux, Rocky Linux, Fedora, Red Hat UBI, Amazon Linux, openSUSE and Arch Linux |

The Secrets job also runs `python3 -m unittest discover -v -s tests/installers -p 'test_*.py'`
to verify that R2 publication sends only validated scripts and their checksums,
rejects corrupt inputs before uploading, and preserves the default download URLs
during recovery. These tests use a local filesystem at the Wrangler boundary.

The jobs run independently. A new run cancels an older run for the same pull
request or branch. Go, Bun, and npm dependencies are cached; installation still
uses the checked-in lockfiles. Go and Bun versions come from `go.mod` and
`web/package.json`. Node.js uses major version 24; Task and golangci-lint versions
are pinned in the workflow, and actions are pinned to commit SHAs.

GitHub Actions retains each step's output. If a packaging job fails, its build
and test logs are also uploaded for seven days. The Compose test includes
container logs on failure and removes only its own project and volumes.

The workflow uploads candidate packages for two days, but does not publish a
release or deploy the website. Other Docker-tagged Agent journeys and tests
requiring explicitly supplied native engine binaries remain separate checks.
See [release publishing](releases.md) and [repository protection](repository-security.md).

Distribution installer checks use fixture executables and replace `systemctl`
at the process boundary because those containers do not boot systemd. They
exercise installation, reinstallation, saved credentials, identity preservation,
file permissions and rejection of corrupt or unchecksummed archives. They do
not claim to verify service startup or SELinux enforcement inside a container.
The two package jobs also install the actual release binaries on their disposable
Ubuntu runners with systemd, create an administrator and Agent key, connect the
Agent, and reinstall both services while checking identity and configuration
preservation. SELinux enforcement still needs validation on an enforcing host.

### Reading workflow logs

Every repository-owned workflow describes its planned checks in English in the
job log, alongside the commands and their output. These descriptions explain
scope; they do not claim that a check passed. Read the individual step results
for the outcome. Explanatory text stays in the logs; the dependency update
report also publishes its actual version findings in the run summary.
Application checks have separate steps so a long Go test run is distinguishable
from formatting, race detection, lint or frontend checks.

Installer test logs use `[TEST]` or `[CHECK]` before a verification and `[PASS]`
after its assertions succeed. Negative cases announce the deliberately invalid
input before running the installer, for example:

```text
[TEST] Intentionally corrupted archive — rejection is expected.
[EXPECTED REJECTION] arveld-v0.0.0-rc.0_linux_amd64.tar.gz: FAILED
[PASS] Intentionally corrupted archive was rejected for the expected reason.
[CHECK] A rejected archive must not replace the installed binary or call systemctl.
```

The test also checks that the rejection has the expected diagnostic. An
unrelated failure or an unexpectedly accepted input fails the job. Missing
checksums and invalid multiline Agent URLs are tested in the same way. The
`[EXPECTED REJECTION]` prefix applies only to these deliberately invalid inputs;
an ordinary installation failing its checksum remains an error.

`v0.0.0-rc.0` is a CI-only version label. Distribution compatibility tests use
tiny fixture executables with this version; native package jobs build real
executables from the commit using the same label. Neither publishes this label
as a release. Prerelease builds receive the actual signed `vX.Y.Z-rc.N` tag instead.

### Workflow responsibilities

| Workflow | What its output explains |
| --- | --- |
| CI | Application, documentation, native packaging, distribution installer tests, workflow validation and secret scanning |
| Release packages | Shared package checks invoked inside CI or Prerelease; real binaries, version checks, Compose/systemd tests and temporary artifacts |
| Prerelease | Signed-tag admission, successful checks on the exact main commit, publication of tested artifacts and conditional attestations |
| Publish Linux installers | Immutable release validation, script allowlist/checksums, stable-only root promotion and public HTTPS verification |
| Frontend dependency audit | Known vulnerability audit of the frontend lockfile; high/critical advisories fail the job |
| CodeQL | Source security analysis for public repositories; a separate explanation states explicitly when private-repository analysis was skipped |
| Dependency update report | Informational engine/frontend version comparisons without changing dependencies |
| Dependabot Updates | GitHub-managed update jobs configured by `.github/dependabot.yml`; these can propose dependency update pull requests |
| Dependency Graph | GitHub-managed jobs that update the dependency inventory used by GitHub security features |

The last two workflows are generated by GitHub rather than YAML files in this
repository, so their step names and log messages cannot be customized here.
They do not run Arveld application tests. SPDX package inventories, dependency
version reports, dependency vulnerability audits and CodeQL source analysis
provide different evidence and are labeled accordingly.

## Ownership rules

| Contract | Location |
| --- | --- |
| Module validation, storage, errors, concurrency and lifecycle | `internal/<module>/*_test.go` |
| HTTP status, headers, cookies, wire shape and admission | `internal/httpapi/*_test.go` |
| Authenticated HTTP, persistence, protocol and CLI journeys | `tests/integration/*_test.go` |
| Injected application lifecycle and configuration | `internal/app/*_test.go` |
| Frontend data/model contracts | `web/tests/` |

Module tests can use temporary databases, local servers and child processes.
Resource use alone does not make a test an application journey.
`testutil.OpenDatabase` only opens, migrates and closes a temporary database;
account/controller fixtures remain in integration test files.

`app.Run` assembles the pinned managed engines. `app.RunWithComponents` accepts
internal Go process dependencies for tests while retaining supervision and
shutdown behavior. These injected dependencies are not an external-engine
configuration mode for deployed Arveld. HTTP stubs can verify ordering and error
handling; they do not prove native engine configuration loading.

## Configuration and Monitor proofs

| Contract | Locations |
| --- | --- |
| Protocol validation, inventory and persistence | `internal/monitor/`, `internal/database/` |
| Deterministic YAML, base preservation and protocol isolation | `internal/configuration/` |
| Atomic publication, input races and managed ownership | `internal/remoteconfig/` |
| Immutable revisions, desired/working/failed separation | `internal/remoteconfig/` |
| Initial base, reconnect, status reports and exact YAML history | `tests/integration/initial_host_metrics_test.go`, `base_reconciliation_test.go`, `controller_config_reports_test.go`, `config_history_test.go` |
| API lifecycle and proactive delivery | `tests/integration/monitor*_test.go`, `network_monitors_test.go`, `internal/opamp/notification_test.go` |
| Browser result and lifecycle semantics | `web/tests/monitor-results.test.ts`, `network-monitors.test.ts`, `http-monitors.test.ts`, `overview.test.ts` |

After building the [Agent image](agent.md), `task test:agent` runs the actual
Collector/Supervisor inventory, startup, compiled YAML validation and execution
journeys. These cover HTTP request bodies and headers, TCP/DNS/ICMP probes,
offline creation and reconnect, Monitor lifecycle and local recovery after a
runtime failure. YAML validation alone does not prove probe execution.
Docker-tagged tests are excluded from the ordinary gate.

The configuration/recovery journey uses the single executable in both
mounted-host mode and native-root mode without `/hostfs`, inside isolated Linux
containers. Both must deliver a real host measurement through the controller's
OTLP admission before the Monitor lifecycle proceeds. Ordinary Agent module tests
also build and start the standalone executable directly on the test host.

## Authentication and persistence

`internal/auth/` and `internal/agentauth/` own validation, digest-only credentials,
expiration, rotation and revocation. Integration journeys cover setup, login,
profile/password changes, session and key management, the password-reset CLI,
OpAMP revocation and OTLP admission. Routing tests verify method/path, origin and
management-access boundaries through the assembled application.

Rule, channel and incident tests use real SQLite to verify persisted definitions,
assignments, migrations, access boundaries and deletion behavior. Stored intent
is separate from engine application and notification delivery. Inspect the test
file for its required upstreams before treating it as a complete runtime proof.

## Native alert publication and evaluation

Tests that need native engines are opt-in. Use the versions pinned in
[the component manifest](../../internal/components/components.lock.json), currently
Prometheus 3.14.0 and Alertmanager 0.34.0. Pass **executable paths**, not server URLs.

```sh
ARVELD_TEST_PROMETHEUS_BINARY=/absolute/path/to/prometheus \
CGO_ENABLED=0 go test -count=1 -v ./tests/integration \
  -run 'TestControllerEvaluatesNativeAlertRulesWithPrometheus|TestControllerPublishesNativeAlertLifecycle|TestControllerDistinguishesMissingDataFromMonitorFailure|TestMetricsQueriesKeepShortDefaultLookback'
```

These fixtures own isolated processes, temporary TSDBs and local listeners. They
verify native pending/firing/recovery, publication after API writes, freshness,
missing-data semantics and query lookback. They ingest controlled telemetry;
they do not execute an Agent or contact probe targets. Without the executable
variable, native tests are skipped. With it, the normal gate also runs them.

Threshold and incident tests in `alert_thresholds_test.go`,
`agent_alert_prometheus_test.go` and `incidents_test.go` cover threshold equality,
stale/invalid measurements, persisted episodes, acknowledgment and restart without
duplicate open incidents. Their native journeys require the same Prometheus
variable. Observe the difference between a condition ending and service recovery.

## Native webhook delivery

```sh
ARVELD_TEST_PROMETHEUS_BINARY=/absolute/path/to/prometheus \
ARVELD_TEST_ALERTMANAGER_BINARY=/absolute/path/to/alertmanager \
CGO_ENABLED=0 go test -count=1 -v ./tests/integration \
  -run '^TestControllerDeliversNativeWebhookLifecycle$'
```

This journey creates a Monitor, channel and rule through the API, ingests an
OTLP failure and receives native webhook JSON. A local receiver rejects the
first request, then verifies retry. After a controller-only restart, a success
measurement produces a resolved notification. Both engine variables are
required. It does not execute an Agent or contact a public recipient.

Other `notification_*_delivery_test.go` journeys use local HTTP/SMTP receivers
to verify email, chat and PagerDuty payloads, grouping, reminders and resolution.
They do not authenticate to provider accounts. The SMTP fixture CA is trusted
only by its test engine; production TLS verification stays enabled.

## Native Monitor silence API

```sh
ARVELD_TEST_ALERTMANAGER_BINARY=/absolute/path/to/alertmanager \
CGO_ENABLED=0 go test -count=1 -v ./tests/integration \
  -run 'TestMonitorSilence.*Boundaries|TestControllerManagesNativeMonitorSilences'
```

This checks input/access boundaries and a native create/read/cancel journey:
exact Monitor scope, pending/active/expired state, controller restart and repeated
cancellation. Directly injected alerts check suppression. It does not include
Prometheus evaluation or message delivery.

## Native Agent silence lifecycle

```sh
ARVELD_TEST_ALERTMANAGER_BINARY=/absolute/path/to/alertmanager \
CGO_ENABLED=0 go test -count=1 -v ./tests/integration -run '^TestAgentSilence'
```

The native journey verifies Agent and Monitor coverage, reads, suppression and
cancellation while another Agent's alerts remain active. The native test skips
without the executable; ordinary API, storage and admission checks still run.

## Native Monitor silence lifecycle

```sh
ARVELD_TEST_PROMETHEUS_BINARY=/absolute/path/to/prometheus \
ARVELD_TEST_ALERTMANAGER_BINARY=/absolute/path/to/alertmanager \
CGO_ENABLED=0 go test -count=1 -v ./tests/integration \
  -run '^TestControllerResumesNativeWebhooksAfterSilence$'
```

This delivery journey checks a future silence becoming active, cancellation and
natural expiration. An unsilenced control delivers while two targets remain
silent; each silenced target can deliver after its window ends. It preserves
production grouping timings and does not promise immediate resending of an
already-delivered alert. Both executable variables are required.

## Manual browser checks

Review the affected workflow on an isolated instance. Browser verification is
manual; the automated frontend gate runs the data/model tests in `web/tests/`.

- Complete the main action and return to the screen that opened it.
- Check loading, empty and failed states; confirm failures retain form input.
- Check keyboard focus, readable labels and charts in English and French at
  desktop, tablet and mobile widths.
- Save screenshots in `output/playwright/` when useful for the review.

Use the [operator journey](setup.md#validate-the-operator-journey) for release
validation. Website-only changes use the
[website verification workflow](documentation.md#verify-changes).

## Release validation

The controller Docker test uses the checked-in Compose recipe and both local
images. It verifies the embedded frontend, initial setup and login, real host
metrics and HTTP Monitor execution by the Agent. After recreating both
containers, it checks sessions, Agent keys, Monitor configuration, historical
metrics, the Agent identity and fresh probe results. Prometheus and Alertmanager
are downloaded and run by the actual controller.
This opt-in test requires Docker and outbound HTTPS for an empty test volume;
it is separate from `task check` and does not exercise a public release download.

Run the [operator journey](setup.md#validate-the-operator-journey) with the actual
distributed binaries and Docker images, including restart and persistence.
Exercise [backup and restoration](../guides/backup-restore.md) in an isolated
instance. Passing source tests alone does not validate packaging, public HTTPS,
a recipient's credentials or notification delivery to that recipient.

Report focused tests, the repository gate, native engine checks, real Agent
execution, browser checks and remote CI separately, with the revision and
artifact versions used.
