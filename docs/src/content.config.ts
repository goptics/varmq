import { defineCollection } from 'astro:content';
import { metaLoader, pageLoader } from '@fumadocs/astro/loaders';
import { pageSchema } from 'fumadocs-core/source/schema';
import { z } from 'astro/zod';

export const collections = {
	docs: defineCollection({
		loader: pageLoader(),
		schema: pageSchema.extend({
			sidebar: z.object({ order: z.number().optional() }).optional(),
		}),
	}),
	meta: defineCollection({ loader: metaLoader() }),
};
