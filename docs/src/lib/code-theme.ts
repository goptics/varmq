// The docs' one code theme. Code is a dark panel in both themes (docs/DESIGN.md, "Dark surfaces"), and the syntax
// colours are the CSS variables `--tk-*`, which ui.css pins to their dark values inside `.codeblock` and the story's
// code. Shiki writes them into each token's style as they are. The fenced blocks (astro.config.mjs), GoPlay and the
// home page's story (lib/story/compile.ts) all highlight with it.
import type { ThemeRegistration } from 'shiki';

const rule = (scope: string, token: string, fontStyle?: string) => ({
	scope,
	settings: { foreground: `var(--tk-${token})`, ...(fontStyle ? { fontStyle } : {}) },
});

export const codeTheme: ThemeRegistration = {
	name: 'varmq-panel',
	type: 'dark',
	colors: {
		'editor.background': 'var(--panel)',
		'editor.foreground': 'var(--panel-ink)',
	},
	settings: [
		{ settings: { foreground: 'var(--panel-ink)' } },
		rule('comment, punctuation.definition.comment', 'c', 'italic'),
		rule(
			'keyword.control, keyword.function, keyword.package, keyword.import, keyword.var, keyword.const, keyword.type, keyword.struct, keyword.interface, keyword.map, keyword.channel, storage.modifier',
			'k',
		),
		rule('string, punctuation.definition.string, constant.character', 's'),
		rule('constant.numeric', 'n'),
		rule('entity.name.function', 'f'),
		rule('entity.name.type', 't'),
		// More specific scopes win: a package name is plain text, and a builtin is a builtin, not a call.
		{ scope: 'entity.name.type.package', settings: { foreground: 'var(--panel-ink)' } },
		rule('entity.name.function.support.builtin, constant.language, storage.type', 'b'),
	],
};
