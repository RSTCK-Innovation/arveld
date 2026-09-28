# Concepts

Arveld monitors services and their supporting machines, and records conditions
that need an operator's attention.

| Term | Meaning |
| --- | --- |
| Controller | The Arveld instance serving the web application, managing configuration, and running metrics and notification engines. |
| Agent | A managed process that reports host measurements and runs Monitors from its own network. |
| Monitor | An HTTP, TCP, DNS or ICMP probe assigned to one Agent. |
| Rule | A sustained condition owned by one Monitor or Agent, with a severity and optional notification destinations. |
| Channel | An email, chat, PagerDuty or webhook destination assigned to alert rules. |
| Incident | A recorded episode of a rule reaching its firing state. Its closure records why the episode ended; it does not by itself establish service recovery. |
| Acknowledgment | An operator's recorded acceptance of responsibility for an incident. It neither closes the incident nor suspends notifications. |
| Silence | A time window that suspends notifications for its target while measurements, evaluation and incident history continue. |

For planned maintenance, schedule a silence. For missing measurements, use a
missing-data rule: missing data is distinct from a measured service failure.

Start with [your first Monitor](../guides/first-monitor.md) and
[notification setup](../guides/notifications.md) to connect these concepts.
