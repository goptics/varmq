import { HomeLayout } from 'fumadocs-ui/layouts/home';
import { Provider, type ProviderProps } from './provider';
import { baseOptions, type Logo } from './layout';

/**
 * The React island of the home page and the 404 page: the header, with the main sections, the search, GitHub and the
 * theme. The page under it is Astro's, a slot React never re-renders (pages/index.astro).
 */
export function Home({ logo, children, ...props }: ProviderProps & { logo: Logo }) {
	return (
		<Provider {...props}>
			<HomeLayout
				{...baseOptions(logo)}
				links={[
					{ text: 'Docs', url: '/start/introduction/', active: 'none' },
					{ text: 'Guides', url: '/guides/worker-types/', active: 'none' },
					{ text: 'Benchmarks', url: '/reference/benchmarks/', active: 'none' },
				]}
			>
				{children}
			</HomeLayout>
		</Provider>
	);
}
