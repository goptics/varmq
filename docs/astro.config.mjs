// @ts-check
import { defineConfig } from 'astro/config';
import sitemap from '@astrojs/sitemap';
import starlight from '@astrojs/starlight';
import { codeSteps } from './src/lib/story/compile.ts';

// The home page's story code, highlighted and keyed for its morph at build time, so the page ships tokens, not Shiki.
const CODE_STEPS = 'virtual:code-steps';

// https://astro.build/config
export default defineConfig({
	site: 'https://varmq.goptics.org',
	vite: {
		plugins: [
			{
				name: 'code-steps',
				resolveId: (id) => (id === CODE_STEPS ? `\0${CODE_STEPS}` : undefined),
				load: async (id) => (id === `\0${CODE_STEPS}` ? `export default ${JSON.stringify(await codeSteps())}` : undefined),
			},
		],
		server: {
			// The repo's assets/ folder sits outside the Astro root (docs/).
			fs: { allow: ['..'] },
		},
	},
	integrations: [
		// The social card's page (src/og/OgCard.astro), for `pnpm og` to screenshot into public/og.png: served by
		// `astro dev` only, so the built site has no such page.
		{
			name: 'og-card',
			hooks: {
				'astro:config:setup': ({ command, injectRoute }) => {
					if (command === 'dev') injectRoute({ pattern: '/og-card', entrypoint: './src/og/OgCard.astro' });
				},
			},
		},
		starlight({
			title: 'VarMQ',
			description: 'A Simplest in-memory and Distributed Storage-Agnostic Message Queue for Your Concurrent Go Program',
			logo: {
				dark: './src/assets/logo-dark.svg',
				light: './src/assets/logo-light.svg',
				replacesTitle: true,
			},
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/goptics/varmq' }],
			// The social card on every page (public/og.png, made by `pnpm og` from src/og/OgCard.astro). Starlight sets
			// the rest of the Open Graph tags and twitter:card summary_large_image.
			head: [
				{ tag: 'meta', attrs: { property: 'og:image', content: 'https://varmq.goptics.org/og.png' } },
				{ tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
				{ tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
				{
					tag: 'meta',
					attrs: { property: 'og:image:alt', content: 'VarMQ: worker pools and job queues for Go, over a map of a worker pool running jobs' },
				},
				{ tag: 'meta', attrs: { name: 'twitter:image', content: 'https://varmq.goptics.org/og.png' } },
			],
			sidebar: [
				{ label: 'Start here', items: [{ autogenerate: { directory: 'start' } }] },
				{ label: 'Core concepts', items: [{ autogenerate: { directory: 'concepts' } }] },
				{ label: 'Worker pool guides', items: [{ autogenerate: { directory: 'guides' } }] },
				{ label: 'Persistence and distribution', items: [{ autogenerate: { directory: 'persistence' } }] },
				{ label: 'Operations', items: [{ autogenerate: { directory: 'operations' } }] },
				{
					label: 'Reference',
					items: [
						{ label: 'API reference', link: 'https://pkg.go.dev/github.com/goptics/varmq' },
						{ label: 'Comparison', slug: 'reference/comparison' },
						{ label: 'Benchmarks', slug: 'reference/benchmarks' },
						{ label: 'Benchmarks repository', link: 'https://github.com/goptics/benchmarks' },
						{ label: 'Bench log', link: 'https://varmq.goptics.org/bench-log/' },
						{ label: 'Changelog', link: 'https://github.com/goptics/varmq/blob/main/CHANGELOG.md' },
					],
				},
			],
			editLink: {
				baseUrl: 'https://github.com/goptics/varmq/edit/main/docs/',
			},
			lastUpdated: true,
			customCss: ['./src/styles/tokens.css', './src/styles/ui.css', './src/styles/docs-theme.css'],
			components: {
				Head: './src/components/shell/Head.astro',
				ThemeSelect: './src/components/ui/ThemeSwitch.astro',
				PageTitle: './src/components/shell/PageTitle.astro',
				Hero: './src/components/shell/Hero.astro',
				Footer: './src/components/shell/Footer.astro',
				SocialIcons: './src/components/shell/SocialIcons.astro',
			},
		}),
		// With a sitemap here, Starlight does not add its own.
		sitemap(),
	],
});
