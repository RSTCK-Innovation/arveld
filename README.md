# Arveld

Arveld is a self-hosted uptime monitor for homelabs and small teams. Monitor
websites, APIs and network services from Agents on your own infrastructure.

- Check HTTP/HTTPS, TCP, ICMP and DNS targets.
- Track availability, response times and host metrics.
- Send alerts through email, Discord, Slack, Microsoft Teams, Telegram,
  PagerDuty or webhooks.
- Keep configuration, incident history and metrics on your infrastructure.

## Documentation

Start with the [user guide](docs/guides/overview.md). It covers
[installation](docs/guides/installation.md), [Agents](docs/guides/agent-setup.md),
[Monitors](docs/guides/first-monitor.md) and
[notifications](docs/guides/notifications.md), followed by HTTPS, backups and
updates.

Read the [documentation at arveld.com](https://arveld.com/docs/). Its source lives
in `docs/`; the website in `website/` publishes that same Markdown alongside the
landing page.

Stable releases provide public Linux `amd64` and `arm64` binaries and GHCR images
on [GitHub Releases](https://github.com/RSTCK-Innovation/arveld/releases/latest).
The [Getting Started convenience scripts](docs/guides/installation.md) install the
latest promoted stable release. To select an exact stable version or release
candidate, follow the [versioned installation instructions](docs/guides/advanced-installation.md#install-a-specific-version).

## Development

See the [codebase map](docs/develop/codebase.md),
[verification guide](docs/develop/verification.md) and
[website guide](docs/develop/documentation.md). The product frontend lives in
`web/`; it is embedded in the controller executable when built.

## Security and contributions

See [SECURITY.md](SECURITY.md) for private vulnerability reporting and
[CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow.

## License

Arveld is licensed under the [Apache License 2.0](LICENSE).
Copyright 2026 RSTCK Innovation. See [NOTICE](NOTICE).
Release archives and images include generated `THIRD_PARTY_NOTICES.txt` files
with the dependency license texts; see [release packaging](docs/develop/releases.md).
Third-party components retain their own copyrights and licenses.
