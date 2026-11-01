// The search index, written at build time to /api/search.json and read by the search dialog (components/fd/search.tsx).
import type { APIRoute } from 'astro';
import { createFromSource } from 'fumadocs-core/search/server';
import { source, withSlash } from '../../lib/source';

const server = createFromSource(source, {
	language: 'english',
	// A result links to the page as the site serves it, with a trailing slash.
	buildIndex: async (page) => ({
		id: page.url,
		url: withSlash(page.url),
		title: page.data.title,
		description: page.data.description,
		structuredData: await page.data.structuredData(),
	}),
});

export const GET: APIRoute = () => server.staticGET();
