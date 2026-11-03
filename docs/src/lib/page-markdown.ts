// What "Copy as Markdown" copies (components/shell/PageTitle.astro): a docs page's MDX as Markdown that makes sense
// outside the site.

const SITE = 'https://varmq.goptics.org';
const REPO_FILES = 'https://raw.githubusercontent.com/goptics/varmq/main';

function prose(text: string, source: (example: string) => string): string {
	return text
		.replace(/^(import|export) .+\n/gm, '')
		.replace(/^<GoPlay example="([^"]+)" \/>$/gm, (_, example: string) => `\`\`\`go\n${source(example).trimEnd()}\n\`\`\``)
		// shortcut: a scene is dropped, so a sentence that points at it dangles; give scenes a text form if readers mind.
		.replace(/^<[A-Z][^>]*\/>\n?/gm, '')
		.replace(/\]\((?:\.\.\/)+assets\//g, `](${REPO_FILES}/assets/`)
		.replace(/\]\(\//g, `](${SITE}/`);
}

/**
 * MDX's `{expression}`s as their values, which a page without `export const` lines does not have: there `{name}` is
 * text. Build time only, on the repository's own pages.
 * shortcut: only `data` is in scope, as benchmarks.mdx is the one page that needs it; pass more when another page does.
 */
function expressions(body: string, data: unknown): string {
	const consts = [...body.matchAll(/^export (const .+)$/gm)].map((m) => m[1]).join('\n');
	if (!consts) return body;
	const value = (expr: string) => String(new Function('data', `${consts}\nreturn (${expr});`)(data));
	return body
		.replace(/<code>\{(.+?)\}<\/code>/g, (_, expr: string) => `\`${value(expr)}\``)
		.replace(/<a href=\{(.+?)\}>(.+?)<\/a>/g, (_, expr: string, label: string) => `[${label}](${value(expr)})`)
		.replace(/^(?!export ).*$/gm, (line) => line.replace(/\{([^{}]+)\}/g, (_, expr: string) => value(expr)));
}

/**
 * The page's Markdown: `mdx` is its file, `source` gives an example program for a GoPlay block, and `data` is what a
 * page's expressions read.
 */
export function pageMarkdown(mdx: string, { title, description }: { title: string; description?: string }, source: (example: string) => string, data?: unknown): string {
	const body = expressions(mdx.replace(/^---\n[\s\S]*?\n---\n/, ''), data);
	// The split keeps each fence at an odd index: Go's `import` and `<-ch` are not MDX, and only the fold meta goes.
	const text = body.split(/^(```[\s\S]*?^```)$/m).map((part, i) => (i % 2 ? part.replace(/ collapse=\{.*?\}/, '') : prose(part, source)));
	const head = description ? `# ${title}\n\n${description}\n\n` : `# ${title}\n\n`;
	return `${head}${text.join('').replace(/\n{3,}/g, '\n\n').trim()}\n`;
}
