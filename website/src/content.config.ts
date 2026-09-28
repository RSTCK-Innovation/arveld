import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";
import { docsSchema } from "@astrojs/starlight/schema";
import { bySlug, bySource, documents, repository } from "./docs/catalog.mjs";

const markdown = glob({
	base: "..",
	pattern: documents.map(({ source }) => source),
	generateId: ({ entry }) => bySource.get(entry)!.slug,
});

export const collections = {
	docs: defineCollection({
		loader: {
			name: "arveld-repository-docs",
			load: (context) =>
				markdown.load({
					...context,
					generateDigest: (contents) =>
						context.generateDigest(contents + JSON.stringify(documents)),
					parseData: ({ id, data, ...rest }) => {
						const page = bySlug.get(id)!;
						return context.parseData({
							id,
							...rest,
							data: {
								...data,
								title: page.title,
								description: page.description,
								editUrl: `${repository}/edit/main/${page.source}`,
							},
						});
					},
				}),
		},
		schema: docsSchema(),
	}),
};
