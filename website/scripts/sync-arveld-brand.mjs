import { readFileSync, writeFileSync, mkdirSync, copyFileSync } from "node:fs";
import { resolve } from "node:path";

const sourceRoot = resolve(process.argv[2] ?? "..");
const theme = readFileSync(resolve(sourceRoot, "web/src/theme.ts"), "utf8");
const value = (expression) => {
	const match = theme.match(expression);
	if (!match) throw new Error(`Theme token not found: ${expression}`);
	return match[1];
};
const tokens = {
	primary: value(/const primary = '(#[a-f\d]+)'/i),
	"primary-dark": value(/primary: \{ main: primary, dark: '(#[a-f\d]+)'/i),
	background: value(/background: \{ default: '(#[a-f\d]+)'/i),
	surface: value(/background: \{ default: '[^']+', paper: '(#[a-f\d]+)'/i),
	text: value(/text: \{ primary: '(#[a-f\d]+)'/i),
	muted: value(/text: \{ primary: '[^']+', secondary: '(#[a-f\d]+)'/i),
	divider: value(/divider: '(#[a-f\d]+)'/i),
	success: value(/success: \{ main: '(#[a-f\d]+)'/i),
	"success-light": value(/success: \{ main: '[^']+', light: '(#[a-f\d]+)'/i),
	error: value(/error: \{ main: '(#[a-f\d]+)'/i),
	"error-light": value(/error: \{ main: '[^']+', light: '(#[a-f\d]+)'/i),
	warning: value(/warning: \{ main: '(#[a-f\d]+)'/i),
};
writeFileSync(
	"src/brand.css",
	`/* Generated from Arveld web/src/theme.ts. */\n:root {\n${Object.entries(
		tokens,
	)
		.map(([key, hex]) => `  --${key}: ${hex};`)
		.join("\n")}\n}\n`,
);
mkdirSync("public/fonts", { recursive: true });
for (const family of ["inter", "manrope"]) {
	const packageRoot = resolve(
		sourceRoot,
		`web/node_modules/@fontsource-variable/${family}`,
	);
	copyFileSync(
		`${packageRoot}/files/${family}-latin-wght-normal.woff2`,
		`public/fonts/${family}.woff2`,
	);
	copyFileSync(`${packageRoot}/LICENSE`, `public/fonts/${family}-LICENSE.txt`);
}
copyFileSync(
	resolve(sourceRoot, "web/public/favicon.svg"),
	"public/arveld-mark.svg",
);
console.log(
	`Synced ${Object.keys(tokens).length} colors, both fonts and the Arveld mark.`,
);
