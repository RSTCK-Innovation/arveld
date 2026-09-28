# Product frontend

The application in `web/` uses React, TypeScript, Vite, TanStack Router/Query,
Material UI and i18next. It supports English and French. The public website and
documentation are a separate application in `website/`.

## Development and serving

Start a [local controller](setup.md#build-and-start-a-local-controller). In a
second terminal, from the repository root:

```sh
task web:install
task web:dev
```

Open [localhost:5173](http://127.0.0.1:5173). Vite proxies `/api` and `/readyz` to
`http://127.0.0.1:8080`; set `ARVELD_API_URL` to use another controller. `bun run
preview` from `web/` uses port 4173 and the same proxy. Servers bind to loopback
and fail if the port is occupied. [package.json](../../web/package.json) and
[.bun-version](../../web/.bun-version) own scripts and tool versions.

`task build` builds `web/dist` and embeds it in the Go executable through
[handler.go](../../web/handler.go). No Vite server, Bun installation or external
asset directory is needed at runtime. Before direct Go build/test commands, run
`task web:build`; repeat after frontend edits.

Production uses the controller's origin behind an HTTPS proxy. Service paths
`/api`, `/v1`, `/healthz` and `/readyz` never fall back to HTML. Browser routes
support GET/HEAD and direct reloads. Missing assets and invalid paths return 404.
The entry document uses `no-store`, hashed assets use a one-year immutable cache,
and the favicon revalidates.

## Source ownership

Paths below are relative to `web/`.

| Location | Responsibility |
| --- | --- |
| `handler.go` | Embedded assets, browser-route fallback and cache policy |
| `src/router.tsx`, `src/pages/` | Navigation and resource workflows |
| `src/data/api.ts`, `session.ts`, `account.ts` | Transport, response schemas, authentication and credentials |
| `src/data/agents.ts`, `agent-metrics.ts` | Agent inventory, configuration reads and host measurements |
| `src/data/monitors.ts`, `monitor-results.ts`, `overview.ts` | Monitor definitions, results, history and overview |
| `src/data/notifications.ts`, `incidents.ts` | Channels, rules, silences, native alert state and incident history |
| `src/data/metrics.ts`, `time-range.ts` | Explorer queries, labels, CSV export and time windows |
| `src/data/instance-health.ts`, `instance-settings.ts` | Readiness and active retention policy |
| `src/data/preferences.ts`, `preferences-query.ts` | Browser preferences and migration |
| `src/components/monitor-wizard.tsx` | Shared Monitor creation/edit form |
| `src/theme.ts`, `src/components/` | Visual identity and reusable presentation |
| `src/i18n/` | Language selection, formatting and catalogs |

## Data and interaction conventions

Inventory, measurements, rules and incidents come from the controller. Queries
distinguish loading, empty, error and unavailable states; they never substitute
example data after a failed request. Session checks run on focus and periodically.
HTTP 401 invalidates shared authentication and private query data. Account
password inputs and newly issued access keys are not stored in Query data or
browser storage. Stored notification configuration, including SMTP credentials
and provider tokens, is returned to the editor and held in the in-memory Query
cache. Query data is not persisted to
browser storage and is cleared when the session is forgotten.

Create and edit workflows preserve entered values on failure. Successful writes
invalidate the relevant inventory, detail and configuration queries. Resource
details and creation pages have reloadable URLs. Destructive actions require
confirmation, and failed writes are not retried automatically.

Agent configuration is read-only in the UI. Revision views display exact stored
YAML as React text/spans, never interpreted HTML. Connection, configuration
application, measurement freshness and notification delivery are separate
outcomes. Use the [configuration contract](agent-configuration.md),
[Monitor reference](../reference/monitors.md),
[metrics reference](../reference/metrics.md) and
[silence reference](../reference/notifications.md#schedule-maintenance) for behavior.

Instance readiness and retention come from live controller reads. Errors hide
previous values until a successful read. Retention is read-only in Settings;
changes require editing the controller YAML and restarting it. Metrics Explorer
runs submitted PromQL queries and retains series labels, missing-data gaps and
zero values; see [query semantics](../reference/metrics.md#metrics-explorer).

## Preferences, language and presentation

Browser preferences use `arveld.preferences.v1` for workspace name and local
refresh. When absent, valid preferences can be copied from `arveld.frontend.v1`
or `arveld.demo.v1`. Those old snapshots remain untouched, and never provide
inventory, metrics or authentication. Failed saves keep the last successful
value and surface the storage error.

Catalogs use English keys with matching placeholders. French translations live
in `src/i18n/locales/fr.json`. Dates, times and numbers follow the selected locale;
language selection also works before login. Code and repository documentation
are written in English.

`theme.ts` owns the product palette, local Inter/Manrope fonts, density and
rounding. Pages compose MUI variants and `sx` layouts. The styled-components
engine is configured in Bun, Vite and TypeScript. There is no Tailwind layer.
Shared controls retain accessible labels and natural content height.

## Verification

From `web/`, `bun run check` runs Biome, Bun tests, TypeScript and the production
build. `task check` from the root includes the Go gate. Follow the
[verification guide](verification.md) to select the relevant automated checks.
Use the [manual browser checklist](verification.md#manual-browser-checks) for
changed UI flows in English and French at desktop, tablet and mobile widths.
