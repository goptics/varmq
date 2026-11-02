// A docs page as Markdown, at its address plus `.md`, written at build time for Copy as Markdown.
import { readFileSync } from 'node:fs';
import type { APIRoute, GetStaticPaths } from 'astro';
import data from '../components/bench/data.json';
import { exampleSource } from '../lib/examples';
import { pageMarkdown } from '../lib/page-markdown';
import { pagePaths } from '../lib/source';

export const getStaticPaths = pagePaths satisfies GetStaticPaths;

export const GET: APIRoute = ({ props: { page } }) =>
	new Response(pageMarkdown(readFileSync(`src/content/docs/${page.path}`, 'utf8'), page.data, exampleSource, data), {
		headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
	});
