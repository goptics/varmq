import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { resolveLinkItems } from 'fumadocs-ui/layouts/shared';
import { FullSearchTrigger } from 'fumadocs-ui/layouts/shared/slots/search-trigger';
import { ThemeSwitch } from 'fumadocs-ui/layouts/shared/slots/theme-switch';
import { Provider, type ProviderProps } from './provider';
import { baseOptions, type Logo } from './layout';

/**
 * The React island of the home page and the 404 page: the header, with the main sections, then GitHub, the theme and
 * the search. Fumadocs' header puts search first and GitHub last, so the three are links here, in reading order: that
 * keeps the tab order and the order on screen the same. The page under it is Astro's, a slot React never re-renders
 * (pages/index.astro).
 */
export function Home({ logo, children, ...props }: ProviderProps & { logo: Logo }) {
	const { githubUrl, ...options } = baseOptions(logo);
	return (
		<Provider {...props}>
			<HomeLayout
				{...options}
				themeSwitch={{ enabled: false }}
				searchToggle={{ components: { lg: <></> } }}
				links={[
					{ text: 'Docs', url: '/start/introduction/', active: 'none' },
					{ text: 'Guides', url: '/guides/worker-types/', active: 'none' },
					{ text: 'Benchmarks', url: '/reference/benchmarks/', active: 'none' },
					...resolveLinkItems({ githubUrl }),
					{
						type: 'custom',
						on: 'nav',
						secondary: true,
						children: (
							<li>
								<ThemeSwitch />
							</li>
						),
					},
					{
						type: 'custom',
						on: 'nav',
						secondary: true,
						children: (
							<li>
								<FullSearchTrigger hideIfDisabled className="w-60 ps-2.5" />
							</li>
						),
					},
					{ type: 'custom', on: 'menu', secondary: true, children: <ThemeSwitch /> },
				]}
			>
				{children}
			</HomeLayout>
		</Provider>
	);
}
