# Security policy

Arveld is in prerelease. Security fixes target the latest release candidate and
`main`; older candidates are not maintained. There is no security response SLA.

## Report a vulnerability

Report vulnerabilities privately using [GitHub private vulnerability reporting](https://github.com/RSTCK-Innovation/arveld/security/advisories/new).
Include the affected version, deployment details, reproduction steps and impact.
Do not include live credentials or data belonging to other people. Do not open a
public issue with exploit details or secrets. If private reporting is unavailable,
open an issue asking for a private contact channel, without vulnerability details.

## Deployment boundary

Keep the controller behind HTTPS, complete administrator setup before exposing it,
and treat Agent keys as credentials. Agents execute network checks and collect
host metrics: install them only on hosts and networks you control. Docker Agents
with a host-root mount can read host files; the mount is read-only, but is not an
isolation boundary. Protect backups, state directories and notification secrets.

See [HTTPS setup](docs/guides/https.md),
[authentication](docs/reference/authentication.md) and
[backups](docs/guides/backup-restore.md).
