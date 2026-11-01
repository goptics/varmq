// Server only: highlights a code block the way the fenced blocks in the docs are highlighted (astro.config.mjs), for
// components that render code from data (GoPlay, the home page's examples).
import { createHighlighter, type Highlighter } from 'shiki';
import { codeTheme } from './code-theme';
import { transformerCollapse } from './code-collapse';

let shiki: Promise<Highlighter> | undefined;

export async function highlight(code: string, lang: string, meta = ''): Promise<string> {
	shiki ??= createHighlighter({ themes: [codeTheme], langs: ['go', 'bash', 'json', 'yaml'] });
	return (await shiki).codeToHtml(code.replace(/\n$/, ''), {
		lang,
		theme: codeTheme.name,
		meta: { __raw: meta },
		transformers: [transformerCollapse()],
	});
}
