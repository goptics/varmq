import { defineCollection } from 'astro:content';
import { metaLoader, pageLoader } from '@fumadocs/astro/loaders';
import { pageSchema } from 'fumadocs-core/source/schema';
import { z } from 'astro/zod';

export const collections = {
	docs: defineCollection({
		loader: pageLoader(),
		schema: pageSchema.extend({
			// The order of the pages lives in each folder's meta.json; `label` names a page in the sidebar (lib/source.ts).
			sidebar: z.object({ order: z.number().optional(), label: z.string().optional() }).optional(),
		}),
	}),
	meta: defineCollection({ loader: metaLoader() }),
};
