import { type SerializedPageTree, useFumadocsLoader } from 'fumadocs-core/source/client';
import type { TOCItemType } from 'fumadocs-core/toc';
import { DocsLayout } from 'fumadocs-ui/layouts/docs';
import { DocsPage } from 'fumadocs-ui/layouts/docs/page';
import { Provider, type ProviderProps } from './provider';
import { baseOptions } from './layout';

export interface DocsProps extends ProviderProps {
	tree: SerializedPageTree;
	toc: TOCItemType[];
}

/** The React island of a docs page. Astro renders the content, so it reaches the island as static HTML (a slot). */
export function Docs({ tree, toc, children, ...props }: DocsProps) {
	const data = useFumadocsLoader({ tree });
	return (
		<Provider {...props}>
			<DocsLayout {...baseOptions()} tree={data.tree}>
				<DocsPage toc={toc}>{children}</DocsPage>
			</DocsLayout>
		</Provider>
	);
}
