// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://varmq.goptics.org',
	vite: {
		server: {
			// The repo's assets/ folder sits outside the Astro root (docs/).
			fs: { allow: ['..'] },
		},
	},
	integrations: [
		starlight({
			title: 'VarMQ',
			description: 'A Simplest in-memory and Distributed Storage-Agnostic Message Queue for Your Concurrent Go Program',
			logo: {
				dark: './src/assets/logo-dark.svg',
				light: './src/assets/logo-light.svg',
				replacesTitle: true,
			},
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/goptics/varmq' }],
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
			customCss: ['./src/styles/tokens.css', './src/styles/docs-theme.css'],
			components: {
				Head: './src/components/shell/Head.astro',
				ThemeProvider: './src/components/shell/ThemeProvider.astro',
				ThemeSelect: './src/components/shell/ThemeSelect.astro',
				PageTitle: './src/components/shell/PageTitle.astro',
				Hero: './src/components/shell/Hero.astro',
				Footer: './src/components/shell/Footer.astro',
			},
		}),
	],
});
