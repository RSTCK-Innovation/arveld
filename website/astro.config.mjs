import { defineConfig } from "astro/config";
import { unified } from "@astrojs/markdown-remark";
import react from "@astrojs/react";
import starlight from "@astrojs/starlight";
import repositoryMarkdown from "./src/docs/remark-repository.mjs";
import { repository, sidebar } from "./src/docs/catalog.mjs";

export default defineConfig({
	site: "https://arveld.com",
	outDir: "./dist/client",
	trailingSlash: "always",
	server: { host: "127.0.0.1", port: 4177 },
	markdown: { processor: unified({ remarkPlugins: [repositoryMarkdown] }) },
	integrations: [
		react(),
		starlight({
			title: "Arveld",
			description:
				"Self-hosted uptime monitoring for websites, APIs and network services.",
			social: [{ icon: "github", label: "GitHub", href: repository }],
			favicon: "/arveld-mark.svg",
			customCss: ["./src/docs/styles.css"],
			components: {
				SiteTitle: "./src/docs/SiteTitle.astro",
				ThemeProvider: "./src/docs/ThemeProvider.astro",
				ThemeSelect: "./src/docs/ThemeSelect.astro",
			},
			expressiveCode: {
				themes: ["github-light"],
				useDarkModeMediaQuery: false,
				shiki: { langAlias: { promql: "text", caddyfile: "text" } },
			},
			sidebar,
		}),
	],
});
