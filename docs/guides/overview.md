# Arveld documentation

Arveld is a self-hosted uptime monitor for websites, APIs and network services.
Install the controller on your infrastructure, connect an Agent, then choose
what to monitor and where to send notifications. The [concepts reference](../reference/concepts.md)
explains Agents, Monitors, rules, incidents and silences.

## Start here

Release binaries and public Docker images are not available yet. The installation
guide describes runtime setup; contributors can use [development setup](../develop/setup.md)
for local validation.

1. [Install Arveld](installation.md) and create the administrator account.
2. [Connect an Agent](agent-setup.md) on the network you want to observe.
3. [Create your first Monitor](first-monitor.md) and inspect its results.
4. [Set up notifications](notifications.md) for failures, missing data or latency.

For a deployment shared with your team, follow [HTTPS and service setup](https.md).

## What runs where

The **controller** serves the web application, stores configuration and incident
history in SQLite, and manages Agents. It runs its own Prometheus for metrics
and alert evaluation, and Alertmanager for notification delivery and silences.
Arveld installs and supervises these engines; you do not configure a separate
Prometheus stack to get started.

An **Agent** runs Monitors from its own network and sends results and host
metrics to the controller. It uses the OpenTelemetry Collector and OpAMP
Supervisor. An Agent in your homelab can check private services that the
controller cannot reach directly.

| Monitor | Typical target |
| --- | --- |
| HTTP / HTTPS | A website, API health endpoint or response body |
| TCP | A database port or another listening service |
| ICMP | A host reachable by ping |
| DNS | DNS lookups through a selected resolver |

Agents also report CPU, memory, disk, network and host uptime measurements.
Alerts can send email, Discord, Slack, Microsoft Teams, Telegram, PagerDuty or
generic webhook notifications.

## Run it over time

- [Persistent data](persistent-data.md): what is stored and where.
- [Back up and restore](backup-restore.md): preserve configuration and history.
- [Update Arveld](updates.md): replace the controller and Agents.
- [Controller configuration](controller.md): settings, retention and service health.

## Work on Arveld

Start with [development setup](../develop/setup.md), the [codebase map](../develop/codebase.md)
and [frontend guide](../develop/frontend.md). To use the product, follow the
[Monitor guide](../reference/monitors.md), [alert workflow](../reference/alerts.md)
and [results guide](../reference/metrics.md). Each guide follows the interface,
with product screenshots showing example data.
