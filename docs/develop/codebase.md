# Codebase map

Arveld consists of a Go controller, an embedded React frontend and a custom
OpenTelemetry Agent distribution. SQLite stores product state; managed
Prometheus and Alertmanager processes own metrics, evaluation and delivery.

| Location | Responsibility |
| --- | --- |
| `cmd/arveld/` | Executable startup, configuration flag and password reset |
| `internal/app/` | Application assembly, process supervision and publication loops |
| `internal/httpapi/` | HTTP routes, authentication boundaries and transport handlers |
| `internal/database/` | SQLite connection and ordered schema migrations |
| `internal/auth/`, `internal/agentauth/` | Administrator/session/management access and separate Agent credentials |
| `internal/agent/`, `internal/opamp/` | Agent inventory and OpAMP protocol |
| `internal/configuration/`, `internal/remoteconfig/` | Desired Agent configuration and revision history |
| `internal/monitor/` | Monitor definitions, validation and lifecycle |
| `internal/alert/`, `internal/notification/` | Rules, incidents, channels, engine publication and silences |
| `internal/components/`, `internal/prometheus/` | Pinned engine installation, supervision and metrics access |
| `web/` | Product UI and Go handler for embedded production assets |
| `agent/` | Separate Go module, standalone Agent entrypoint, component inventory and Docker build |
| `Dockerfile`, `compose.yaml`, `docker/` | Controller image, local Compose installation and container defaults |
| `tests/` | Cross-package integration and native-engine verification |
| `docs/` | User guides, references and developer documentation |
| `website/` | Static public site and documentation renderer |

## Change a workflow

Start at the relevant route in `internal/httpapi/`, follow its domain package and
storage, then update the corresponding frontend data module and page. Keep Agent
credentials distinct from management access. A persisted Monitor or rule and a
successfully applied Agent/engine configuration are separate outcomes.

Read the [Agent configuration contract](agent-configuration.md) before changing
compilation, delivery, revision history or recovery. The [test guide](verification.md)
explains which checks prove each boundary.

## Source of truth

Executable manifests and current code own versions, defaults and routes.
Documentation describes the implemented behavior and belongs in `docs/`.
Keep operator procedures under `guides/`, product usage and concepts under
`reference/`, and build/architecture/verification material under `develop/`.
The [website catalog](../../website/src/docs/catalog.mjs) publishes these sources
directly; component directories do not maintain separate README copies.
