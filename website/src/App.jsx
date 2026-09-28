import { useRef, useState } from "react";
import {
	ArrowRight,
	ArrowLeft,
	ArrowUpRight,
	Play,
	X,
	MagnifyingGlassPlus,
	TerminalWindow,
	BookOpen,
	GithubLogo,
} from "@phosphor-icons/react";

const REPOSITORY = "https://github.com/RSTCK-Innovation/arveld";
const DOCS = "/docs/";
const INSTALL = "/docs/get-started/installation/";
const steps = [
	{
		label: "Monitors",
		title: "Monitor results",
		image: "/product/monitors.png",
		description:
			"Monitor websites and APIs with HTTP/HTTPS, service ports with TCP, hosts with ICMP, and DNS lookups. Your chosen Agent runs each Monitor and reports availability and response time.",
		alt: "Arveld Monitors screen with HTTP, TCP, ICMP and DNS services, availability histories and response times.",
	},
	{
		label: "Monitor detail",
		title: "Status and latency history",
		image: "/product/monitor-detail.png",
		description:
			"Open a Monitor to inspect its latest measurement, status history and latency. See when the service stopped responding and how response times changed.",
		alt: "An HTTP Monitor in Arveld showing its latest measurement, status history, latency chart and monitoring rules section.",
	},
	{
		label: "Agents",
		title: "Agent status and host metrics",
		image: "/product/agents.png",
		description:
			"Agents report host metrics as well as Monitor results. Open an Agent to see its connection status, CPU, memory, disk usage and host uptime.",
		alt: "An Arveld Agent showing its Connected status, CPU, memory and disk gauges, host uptime and resource charts.",
	},
	{
		label: "Notifications",
		title: "Notification channel configuration",
		image: "/product/notifications.png",
		description:
			"Choose email, Discord, Slack, Telegram, Microsoft Teams, PagerDuty or a webhook. Create the channel, assign it to an Agent or Monitor rule, and receive alerts and recovery updates.",
		alt: "The Arveld notification channel editor with a Discord destination and alert grouping settings.",
	},
	{
		label: "Incidents",
		title: "Incident history",
		image: "/product/incidents.png",
		description:
			"See observed alert episodes, filter by status and severity, and open an incident to record who is taking care of it.",
		alt: "Arveld incident history with an open critical Monitor latency alert for a Home dashboard.",
	},
];

function ProductImage({ image, alt, onOpen }) {
	return (
		<button
			className="product-image"
			onClick={onOpen}
			aria-label={`Enlarge screenshot: ${alt}`}
		>
			<img src={image} alt={alt} loading="lazy" decoding="async" />
			<span className="image-expand">
				<MagnifyingGlassPlus size={16} /> View screenshot
			</span>
		</button>
	);
}

export function App() {
	const [step, setStep] = useState(0);
	const [tour, setTour] = useState(true);
	const [fullSize, setFullSize] = useState(false);
	const dialogRef = useRef(null);
	const current = steps[step];

	function openTour(index = 0, guided = true) {
		selectStep(index);
		setTour(guided);
		dialogRef.current.showModal();
	}
	function selectStep(index) {
		setStep(index);
		setFullSize(false);
		dialogRef.current.querySelector(".tour-image-scroll").scrollTo(0, 0);
	}
	function moveStep(direction) {
		selectStep(Math.max(0, Math.min(steps.length - 1, step + direction)));
	}

	return (
		<div className="landing">
			<a className="skip-link" href="#main">
				Skip to content
			</a>
			<header className="site-header wrap">
				<a className="brand" href="#main" aria-label="Arveld, home">
					<img src="/arveld-mark.svg" width="34" height="34" alt="" />
					<span>arveld.</span>
				</a>
				<nav aria-label="Main navigation">
					<a href={DOCS}>Documentation</a>
					<a href={REPOSITORY} target="_blank" rel="noreferrer">
						GitHub <ArrowUpRight size={13} />
					</a>
				</nav>
			</header>
			<main id="main" className="wrap">
				<section className="hero" aria-labelledby="hero-title">
					<div>
						<p className="eyebrow">An open-source project</p>
						<h1 id="hero-title">
							Self-hosted
							<br />
							<span>uptime monitoring.</span>
						</h1>
						<div className="actions">
							<a className="button-primary" href={INSTALL}>
								Installation guide <ArrowRight size={17} />
							</a>
							<button className="text-link" onClick={() => openTour()}>
								<Play size={15} /> Take a tour
							</button>
						</div>
					</div>
					<div className="hero-copy">
						<p className="hero-lead">
							Arveld monitors websites, APIs and network services.
						</p>
						<p>
							Track availability, response times and host metrics. Send alerts
							by email, chat or webhook.
						</p>
						<p className="audience">
							For homelabs and small teams. Runs on your infrastructure.
						</p>
					</div>
				</section>

				<nav className="page-outline" aria-label="Explore Arveld">
					<a href="#monitors">
						<span>01</span> Monitors <ArrowRight size={15} />
					</a>
					<a href="#agents">
						<span>02</span> Agents <ArrowRight size={15} />
					</a>
					<a href="#notifications">
						<span>03</span> Notifications <ArrowRight size={15} />
					</a>
				</nav>

				<section
					id="monitors"
					className="feature-section monitors-section"
					aria-labelledby="monitors-title"
				>
					<div className="section-intro">
						<div className="feature-copy">
							<p className="eyebrow">01 / Monitors</p>
							<h2 id="monitors-title">
								Availability and <br />
								response times.
							</h2>
						</div>
						<div className="intro-detail">
							<p>
								A Monitor checks an endpoint at a configured interval, using
								HTTP/HTTPS, TCP, ICMP or DNS. It runs from an assigned Agent,
								which can reach public services or endpoints on your network.
							</p>
							<button className="text-link" onClick={() => openTour(1)}>
								View Monitor details <ArrowRight size={16} />
							</button>
						</div>
					</div>
					<dl className="monitor-types">
						<div>
							<dt>HTTP / HTTPS</dt>
							<dd>
								Websites & APIs
								<span>Your website, dashboard or health endpoint.</span>
							</dd>
						</div>
						<div>
							<dt>TCP</dt>
							<dd>
								Database & service ports
								<span>Connections to PostgreSQL, Redis or SSH.</span>
							</dd>
						</div>
						<div>
							<dt>ICMP</dt>
							<dd>
								Servers & network devices
								<span>Ping a server, router or NAS.</span>
							</dd>
						</div>
						<div>
							<dt>DNS</dt>
							<dd>
								DNS resolution
								<span>Query the resolver and record type you choose.</span>
							</dd>
						</div>
					</dl>
				</section>

				<section
					id="agents"
					className="feature-section agents-section"
					aria-labelledby="agents-title"
				>
					<div className="feature-copy">
						<p className="eyebrow">02 / Agents</p>
						<h2 id="agents-title">
							Monitor execution <br />
							and host metrics.
						</h2>
						<p>
							Run one controller and one or more Agents. Each Agent executes its
							assigned Monitors and reports host metrics. The controller
							centralizes their results and manages configuration and alerts.
						</p>
						<ul className="plain-list">
							<li>Agent connection status</li>
							<li>CPU, memory, disk and network metrics</li>
							<li>Host uptime and resource history</li>
						</ul>
						<button className="text-link" onClick={() => openTour(2)}>
							View Agent details <ArrowRight size={16} />
						</button>
					</div>
					<figure>
						<ProductImage {...steps[2]} onOpen={() => openTour(2, false)} />
						<figcaption>
							An Agent’s connection status and host metrics, in the product.
						</figcaption>
					</figure>
				</section>

				<section
					id="notifications"
					className="feature-section notifications-section"
					aria-labelledby="notifications-title"
				>
					<div className="section-intro">
						<div className="feature-copy">
							<p className="eyebrow">03 / Alerts & notifications</p>
							<h2 id="notifications-title">
								Alert rules and <br />
								notification channels.
							</h2>
						</div>
						<div className="intro-detail">
							<p>
								Rules evaluate Monitor failures, missing data, latency or host
								resource usage. Each rule sends updates to the notification
								channels assigned to it.
							</p>
						</div>
					</div>
					<ul
						className="channel-list"
						aria-label="Supported notification channels"
					>
						{[
							"Email",
							"Discord",
							"Slack",
							"Telegram",
							"Microsoft Teams",
							"PagerDuty",
							"Webhook",
						].map((channel) => (
							<li key={channel}>{channel}</li>
						))}
					</ul>
					<div className="notification-body">
						<div className="feature-copy">
							<ol className="notification-flow">
								<li>
									<strong>Create a channel.</strong> Connect your inbox, chat or
									webhook.
								</li>
								<li>
									<strong>Add it to a rule.</strong> Pick a Monitor or Agent and
									the condition to watch.
								</li>
								<li>
									<strong>Receive updates.</strong> Arveld sends a notification
									when the condition fires and when it ends.
								</li>
							</ol>
							<p className="feature-note">
								Group alerts, set reminder intervals and pause notifications
								during maintenance.
							</p>
							<button className="text-link" onClick={() => openTour(3)}>
								View channel settings <ArrowRight size={16} />
							</button>
						</div>
						<figure>
							<ProductImage {...steps[3]} onOpen={() => openTour(3, false)} />
							<figcaption>
								A Discord channel in Arveld, with alert grouping and delivery
								intervals.
							</figcaption>
						</figure>
					</div>
				</section>

				<nav className="project-resources" aria-label="Project resources">
					<a className="resource-link" href={INSTALL}>
						<TerminalWindow size={22} aria-hidden="true" />
						<strong>
							Install Arveld <ArrowUpRight size={15} />
						</strong>
						<span>Run and configure the controller.</span>
					</a>
					<a className="resource-link" href={DOCS}>
						<BookOpen size={22} aria-hidden="true" />
						<strong>
							Read the docs <ArrowUpRight size={15} />
						</strong>
						<span>Setup guides and technical reference.</span>
					</a>
					<a
						className="resource-link"
						href={REPOSITORY}
						target="_blank"
						rel="noreferrer"
					>
						<GithubLogo size={22} aria-hidden="true" />
						<strong>
							Browse the source <ArrowUpRight size={15} />
						</strong>
						<span>Read the code, report issues or contribute.</span>
					</a>
				</nav>
			</main>
			<footer className="site-footer wrap">
				<p>arveld.</p>
				<span>Open-source, self-hosted uptime monitoring.</span>
				<a href={REPOSITORY} target="_blank" rel="noreferrer">
					Source on GitHub <ArrowUpRight size={13} />
				</a>
			</footer>

			<dialog
				ref={dialogRef}
				className={`product-tour ${fullSize ? "is-full-size" : ""}`}
				aria-labelledby="tour-title"
				onClick={(event) => {
					if (event.target === event.currentTarget) dialogRef.current.close();
				}}
			>
				<div className="tour-header">
					<div>
						<p className="eyebrow">
							{tour
								? `Product tour / ${String(step + 1).padStart(2, "0")} of ${String(steps.length).padStart(2, "0")}`
								: "Product screenshot"}
						</p>
						<h2 id="tour-title">{current.title}</h2>
					</div>
					<button
						className="icon-button"
						onClick={() => dialogRef.current.close()}
						aria-label="Close product tour"
						autoFocus
					>
						<X size={22} />
					</button>
				</div>
				{tour && (
					<div className="tour-steps" role="group" aria-label="Tour steps">
						{steps.map((item, index) => (
							<button
								key={item.label}
								aria-pressed={index === step}
								onClick={() => selectStep(index)}
							>
								{item.label}
							</button>
						))}
					</div>
				)}
				<div
					className="tour-image-scroll"
					tabIndex={0}
					aria-label="Product screenshot. Scroll to inspect."
				>
					<img key={current.image} src={current.image} alt={current.alt} />
				</div>
				<div className="tour-footer">
					<p aria-live="polite">
						{current.description}
						<span>Actual product capture · example data</span>
					</p>
					<div className="tour-controls">
						<button
							className="zoom-control"
							onClick={() => setFullSize((value) => !value)}
							aria-pressed={fullSize}
							aria-label={
								fullSize
									? "Fit screenshot to window"
									: "Show screenshot at original size"
							}
						>
							<MagnifyingGlassPlus size={20} />
							<span>{fullSize ? "Fit image" : "Zoom in"}</span>
						</button>
						{tour && (
							<>
								<button
									className="icon-button"
									disabled={step === 0}
									onClick={() => moveStep(-1)}
									aria-label="Previous tour step"
								>
									<ArrowLeft size={20} />
								</button>
								<button
									className="button-primary"
									onClick={() =>
										step === steps.length - 1
											? dialogRef.current.close()
											: moveStep(1)
									}
								>
									{step === steps.length - 1 ? "Finish tour" : "Next"}
									<ArrowRight size={17} />
								</button>
							</>
						)}
					</div>
				</div>
			</dialog>
		</div>
	);
}
