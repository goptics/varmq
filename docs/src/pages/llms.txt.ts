// /llms.txt: the docs pages as a Markdown list, each linked to its `.md` form, for tools that read docs as text.
import type { APIRoute } from 'astro';
import { source } from '../lib/source';

const site = 'https://varmq.goptics.org';

export const GET: APIRoute = () => {
	const lines = source
		.getPages()
		.map((p) => `- [${p.data.title}](${site}${p.url}.md): ${p.data.description ?? ''}`)
		.sort();
	const body = [
		'# VarMQ',
		'',
		'> A typed worker pool and job queue for Go. Jobs live in memory, or in Redis or SQLite so they survive a restart.',
		'',
		'## Docs',
		'',
		...lines,
		'',
	].join('\n');
	return new Response(body, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
};
