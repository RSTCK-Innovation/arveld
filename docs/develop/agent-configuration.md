# Managed Agent configuration

This document describes how Arveld compiles, publishes and tracks configuration
for each Agent. For installation, use [the Agent guide](../guides/agent-setup.md).

## Representations and ownership

`current Arveld base + current assigned Monitors → specification → Collector YAML → immutable artifact`

| Representation | Owner | Purpose |
| --- | --- | --- |
| Base policy | Installed Arveld release | Host collection, identity and export |
| Product Monitor | `monitor` | Name, protocol settings and Agent assignment |
| Compilation specification | `configuration` | Resolved execution inputs for one Agent |
| Collector document | Private compiler implementation | Receivers, processors, exporters and pipeline references |
| Published revision | `remoteconfig` | Exact immutable bytes, hash, historical specification and creation date |
| Application report | Agent, persisted through `remoteconfig` | Observation of the reported artifact |

[Specification](../../internal/configuration/configuration.go) is an Arveld-owned
JSON shape, independent of upstream receiver configuration types. Schema version,
base version, published revision and byte hash answer different questions.
The specification schema is 1; the base version is 4. Historical
specifications describe their artifact and do not pin an Agent to an old base.
When compiler output changes, invalidate reconciliation inputs by updating the
base version. Keep compatibility of persisted specifications explicit when
changing these formats.

Product names/assignments remain in Monitor storage. Execution settings are copied
into immutable compilation inputs by [inputs.go](../../internal/remoteconfig/inputs.go).
The specification is not a second editable inventory. Removing or reassigning a
Monitor removes its contribution from the old Agent's next managed configuration.

## Compilation

[Compile](../../internal/configuration/configuration.go) and protocol contributions:

- Rebuild from the current base and complete Monitor set; preserve the base and
  unrelated contributions. Each Monitor has one receiver, identity processor and
  metrics pipeline referencing the shared exporter.
- Derive component names from stable Monitor IDs, not editable names. Validate
  protocol settings, component references and identity collisions.
- Order output deterministically. Equivalent input ordering yields identical
  bytes; base-only output retains the established base bytes.
- Preserve `${env:ARVELD_URL}`, `${env:ARVELD_AGENT_TOKEN}` and the host-root
  reference without resolving the Agent environment. Base 4 uses
  `${env:ARVELD_HOST_ROOT:-/hostfs}`: the standalone executable defaults this
  variable to `/`, the Docker image sets `/hostfs`, and the fallback preserves
  the mount used by older Docker Agents. Escape literal dollars in HTTP endpoints, headers, bodies and assertions for the
  Collector resolver. Compilation performs no SQL or network operations.
- Use private Collector document types, `confmap` conversion and `component.ID`
  validation; keep upstream types out of the persisted product contract.

The base collects CPU, memory, filesystem, network counters and system uptime and exports through
Arveld with Agent identity. The [results guide](../reference/metrics.md) explains
how measurements, freshness and gaps appear in the interface. Metric mappings
and chart calculations live in the [frontend data modules](../../web/src/data/).

The compiler targets the single [Arveld Agent distribution](../../agent/collector-builder.yaml).
The specification contains no executable name/version and publication does not
check the installed executable's compatibility. An incompatible Agent reports
application failure. OpAMP configuration delivery does not upgrade that executable.
Use [the Agent guide](../guides/agent-setup.md) for installation and upgrades.

## Reconciliation and concurrency

[ReconcileAgent](../../internal/remoteconfig/reconcile.go) runs after each Monitor
mutation and on eligible OpAMP messages, including reconnect and HTTP polling.
It creates initial managed configuration when no assignment exists.

1. Read all assigned Monitors in stable ID order and compile the current base
   plus their execution settings outside a write transaction.
2. Begin SQLite's immediate transaction; reread the complete ordered Monitor
   snapshot and reject changed inputs with `ErrInputsChanged`.
3. Check managed ownership and previously evaluated inputs under that same lock.
   Unsupported schemas, future bases or unrelated Agent identities stop publication.
4. Atomically create/reuse an immutable artifact, choose the desired revision and
   record evaluated inputs. Identical YAML reuses its revision without rewriting
   its historical specification/date; changed inputs still become reconciled.
5. After commit, HTTP mutation handling attempts OpAMP notification. Compilation
   or delivery failure does not undo the already committed Monitor mutation.

An update reads the previous Agent in its write transaction; deletion atomically
returns the removed definition. Reassignment reconciles old and new Agents.
Transfer is asynchronous: an offline old Agent may continue its last configuration
until reconnect. No exclusive-execution handoff is promised.

Legacy assignments without provenance, explicit YAML writes and manual rollback
remain outside managed reconciliation. No YAML classification/adoption occurs.
A later eligible Agent message recovers unfinished reconciliation from current
persisted intent. There is no startup sweep or autonomous retry worker today.

## Desired, reported, working and failed

[Status handling](../../internal/remoteconfig/status.go) retains distinct state:

- `desired`: the target selected by the controller.
- `reported`: the latest Agent observation, which may refer to another revision.
- `last_working`: a known artifact positively reported `APPLIED`.
- The desired target's persistent failure marker and separate `last_failure` history.

`FAILED` never changes desired intent. Supervisor recovery can report an earlier
working artifact while the desired target stays failed. Applying, failed and
unknown-hash reports do not invent a working confirmation. Reconciliation runs
before report recording so an initial matching applied report can establish one.

Failed targets are suppressed across reconnect and controller restart. Selecting
different content releases the block; saving identical bytes does not retry it.
A later applied report for the desired hash clears its block while preserving
failure history. Status uses independent reads rather than promising a transaction-wide
snapshot; desired artifact and its active failure marker are read together.

An omitted OpAMP configuration report retains the live connection's previous
report; an explicit empty report replaces it. This cache prevents health-only
messages from resending an in-progress configuration. Connection close removes
it. It is not full-state resynchronization or replaced-session fencing.

## Notification delivery

[NotifyAgentConfig](../../internal/opamp/notification.go) rereads committed intent
for eligible WebSockets. Plain HTTP Agents receive it on the next poll.
Each call has one five-second batch deadline, including waits for busy sessions,
independent of cancellation of the originating management request.

A capacity-one channel gate serializes target selection and sending for each
connection. The callback and proactive notification use that same gate; the target
is read after acquiring it. This prevents a queued old offer from following a
newer one. The global registry lock is released before network writes.

Admission rechecks the connection's Agent key and advertised configuration
capability. A blocked upstream send is bounded by closing the connection when
its context expires. A successful socket send is not proof of application.
Failure preserves committed intent for later convergence.

## History and diagnostics

[History](../../internal/remoteconfig/history.go) returns descending revision
metadata and exact YAML scoped to an Agent. HTTP request bodies and header values
are sensitive configuration retained in these revisions; authorized configuration
readers can retrieve them under the existing access model. Creation dates are immutable and may
be unknown for older revisions. UI reads never select a desired target.

The UI exposes status and revision history as read-only views. It does not edit
YAML, switch the desired revision or retry a failed artifact. Follow the
[configuration troubleshooting guide](diagnostics.md) to inspect a failure and
correct the associated Monitor settings through the interface.

## Verification

Use [the verification guide](verification.md#configuration-and-monitor-proofs)
for module, controller/OpAMP and real Supervisor/Collector checks. Preserve
determinism, unchanged history, stale-input rejection, recovery and failed-target
suppression. Runtime compatibility is checked by tests using the pinned
executables, not by a per-Agent compatibility check before publication.
