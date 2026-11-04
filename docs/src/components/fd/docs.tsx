import { type SerializedPageTree, useFumadocsLoader } from 'fumadocs-core/source/client';
import type { TOCItemType } from 'fumadocs-core/toc';
import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import { DocsPage } from 'fumadocs-ui/layouts/docs/page';
import { Provider, type ProviderProps } from './provider';
import { baseOptions, type Logo } from './layout';

export interface DocsProps extends ProviderProps {
	tree: SerializedPageTree;
	toc: TOCItemType[];
	logo: Logo;
}

/**
 * The React island of a docs page: the sidebar, the table of contents and the search. Astro renders the page itself
 * (pages/[...slug].astro), with its title, previous and next links and footer, so it reaches the island as static HTML
 * (a slot) that React never re-renders.
 */
export function Docs({ tree, toc, logo, children, ...props }: DocsProps) {
	const data = useFumadocsLoader({ tree });
	return (
		<Provider {...props}>
			<DocsLayout {...baseOptions(logo)} tree={data.tree}>
				<DocsPage toc={toc} footer={{ enabled: false }} breadcrumb={{ enabled: false }}>
					{children}
				</DocsPage>
			</DocsLayout>
		</Provider>
	);
}
