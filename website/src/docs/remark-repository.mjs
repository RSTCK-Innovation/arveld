import { existsSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { visit } from "unist-util-visit";
import { bySource, repository } from "./catalog.mjs";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const publicRoot = "website/public/";

export default function repositoryMarkdown() {
	return (tree, file) => {
		const source = path.relative(root, file.path).split(path.sep).join("/");
		if (!bySource.has(source)) return;
		// Starlight already supplies the page title.
		if (tree.children[0]?.type === "heading" && tree.children[0].depth === 1)
			tree.children.shift();
		visit(tree, (node) => {
			if (!["link", "image", "definition"].includes(node.type)) return;
			if (/^(?:[a-z][a-z\d+.-]*:|\/|#)/i.test(node.url)) return;
			const [, pathname, suffix = ""] = node.url.match(/^([^#?]*)(.*)$/);
			const target = path.posix.normalize(
				path.posix.join(path.posix.dirname(source), decodeURI(pathname)),
			);
			const page = bySource.get(target);
			if (page) node.url = `/${page.slug}/${suffix}`;
			else if (target.startsWith(publicRoot))
				node.url = `/${target.slice(publicRoot.length)}${suffix}`;
			else {
				const absolute = path.join(root, target);
				if (!existsSync(absolute))
					file.fail(`Broken repository link: ${target}`);
				const kind = statSync(absolute).isDirectory() ? "tree" : "blob";
				node.url = `${repository}/${kind}/main/${target}${suffix}`;
			}
		});
	};
}
