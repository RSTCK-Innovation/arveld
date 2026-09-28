export const repository = "https://github.com/RSTCK-Innovation/arveld";

// Repository Markdown remains canonical. Only publication metadata lives here.
export const groups = [
	{
		label: "Get started",
		pages: [
			[
				"docs/guides/overview.md",
				"docs",
				"Overview",
				"Understand Arveld and follow the installation, Agent and Monitor guides.",
			],
			[
				"docs/guides/installation.md",
				"docs/get-started/installation",
				"Install Arveld",
				"Prepare the controller, create the administrator and check readiness.",
			],
			[
				"docs/guides/agent-setup.md",
				"docs/get-started/agents",
				"Connect an Agent",
				"Connect an Agent, preserve its identity and choose its monitoring network.",
			],
			[
				"docs/guides/first-monitor.md",
				"docs/get-started/first-monitor",
				"Your first Monitor",
				"Create an HTTP, TCP, ICMP or DNS Monitor and read its results.",
			],
			[
				"docs/guides/notifications.md",
				"docs/get-started/notifications",
				"Set up notifications",
				"Connect notification channels to Agent and Monitor alert rules.",
			],
		],
	},
	{
		label: "Using Arveld",
		pages: [
			[
				"docs/reference/concepts.md",
				"docs/reference/concepts",
				"Concepts",
				"Understand controllers, Agents, Monitors, rules, incidents and silences.",
			],
			[
				"docs/reference/monitors.md",
				"docs/reference/monitors",
				"Monitors",
				"Find, edit and configure Monitors through the interface.",
			],
			[
				"docs/reference/alerts.md",
				"docs/reference/alerts",
				"Alert rules and incidents",
				"Create alert rules, read their state and follow incidents in the interface.",
			],
			[
				"docs/reference/notifications.md",
				"docs/reference/notifications",
				"Channels and maintenance",
				"Configure notification channels and schedule maintenance through the interface.",
			],
			[
				"docs/reference/metrics.md",
				"docs/reference/metrics",
				"Metrics and results",
				"Read Monitor results, inspect host metrics and use the Explorer.",
			],
			[
				"docs/reference/authentication.md",
				"docs/reference/authentication",
				"Account and access",
				"Manage your profile, password and access keys in Arveld.",
			],
		],
	},
	{
		label: "Operate Arveld",
		pages: [
			[
				"docs/guides/advanced-installation.md",
				"docs/operations/installation",
				"Other installation methods",
				"Install an exact release, use Docker or run the controller and Agents manually.",
			],
			[
				"docs/guides/https.md",
				"docs/operations/https",
				"HTTPS and service setup",
				"Run Arveld behind an HTTPS reverse proxy as a Linux service.",
			],
			[
				"docs/guides/controller.md",
				"docs/operations/configuration",
				"Controller configuration",
				"Configure retention, operation timeouts and controller health checks.",
			],
			[
				"docs/guides/persistent-data.md",
				"docs/operations/persistent-data",
				"Persistent data",
				"Locate SQLite, metrics, engine configuration and Agent state.",
			],
			[
				"docs/guides/backup-restore.md",
				"docs/operations/backup-restore",
				"Back up and restore",
				"Back up the complete controller and restore it in isolation.",
			],
			[
				"docs/guides/updates.md",
				"docs/operations/updates",
				"Updates",
				"Replace the controller and Agents while preserving their data.",
			],
		],
	},
	{
		label: "Develop",
		collapsed: true,
		pages: [
			[
				"docs/develop/setup.md",
				"docs/develop/setup",
				"Development setup",
				"Build Arveld and update pinned managed components.",
			],
			[
				"docs/develop/codebase.md",
				"docs/develop/codebase",
				"Codebase map",
				"Find the packages and manifests that own each part of Arveld.",
			],
			[
				"docs/develop/frontend.md",
				"docs/develop/frontend",
				"Product frontend",
				"Develop the React frontend and understand its connected workflows.",
			],
			[
				"docs/develop/agent.md",
				"docs/develop/agent",
				"Agent development",
				"Build the custom Collector and Supervisor image for local development.",
			],
			[
				"docs/develop/agent-configuration.md",
				"docs/develop/agent-configuration",
				"Agent configuration",
				"Understand compilation, reconciliation, delivery and recovery.",
			],
			[
				"docs/develop/diagnostics.md",
				"docs/develop/diagnostics",
				"Configuration diagnostics",
				"Inspect and diagnose Agent configuration and application failures.",
			],
			[
				"docs/develop/verification.md",
				"docs/develop/verification",
				"Verification",
				"Select the existing controller, engine and Agent verification commands.",
			],
			[
				"docs/develop/releases.md",
				"docs/develop/releases",
				"Release candidates",
				"Build, test and publish signed release candidates.",
			],
			[
				"docs/develop/installer-hosting.md",
				"docs/develop/installer-hosting",
				"Installer hosting",
				"Publish Linux installation scripts to Cloudflare R2 at install.arveld.com.",
			],
			[
				"docs/develop/repository-security.md",
				"docs/develop/repository-security",
				"Repository protection",
				"Maintain repository protections and prepare public access.",
			],
			[
				"docs/develop/documentation.md",
				"docs/develop/documentation",
				"Documentation website",
				"Edit canonical Markdown and build the Astro and Starlight website.",
			],
		],
	},
];

export const documents = groups.flatMap(({ pages }) =>
	pages.map(([source, slug, title, description]) => ({
		source,
		slug,
		title,
		description,
	})),
);
export const bySource = new Map(documents.map((page) => [page.source, page]));
export const bySlug = new Map(documents.map((page) => [page.slug, page]));
export const sidebar = groups.map(({ label, collapsed, pages }) => ({
	label,
	collapsed,
	items: pages.map(([, slug, label]) => ({ slug, label })),
}));
