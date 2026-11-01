// @ts-check
import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import sitemap from '@astrojs/sitemap';
import tailwindcss from '@tailwindcss/vite';
import fumadocs from '@fumadocs/astro';
import { satteri } from '@astrojs/markdown-satteri';
import { remarkDirectiveAdmonition } from '@fumadocs/satteri/remark-directive-admonition';
import { codeSteps } from './src/lib/story/compile.ts';
import { codeTheme } from './src/lib/code-theme.ts';
import { transformerCollapse } from './src/lib/code-collapse.ts';

const CODE_STEPS = 'virtual:code-steps';

export default defineConfig({
	site: 'https://varmq.goptics.org',
	trailingSlash: 'always',
	// `:::note` and `:::caution` are directives (remarkDirectiveAdmonition below).
	markdown: { processor: satteri({ features: { gfm: true, directive: true } }) },
	vite: {
		plugins: [
			tailwindcss(),
			{
				name: 'code-steps',
				resolveId: (id) => (id === CODE_STEPS ? `\0${CODE_STEPS}` : undefined),
				load: async (id) => (id === `\0${CODE_STEPS}` ? `export default ${JSON.stringify(await codeSteps())}` : undefined),
			},
		],
		server: { fs: { allow: ['..'] } },
	},
	integrations: [
		react(),
		fumadocs({
			collections: {
				docs: {
					dir: 'src/content/docs',
					mdastPlugins: [
						remarkDirectiveAdmonition({ types: { note: 'info', tip: 'success', caution: 'warning', danger: 'error' } }),
					],
					rehypeCodeOptions: { themes: { dark: codeTheme }, defaultColor: 'dark', transformers: [transformerCollapse()] },
				},
			},
		}),
		sitemap(),
	],
});
