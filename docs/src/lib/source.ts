import { getCollection } from 'astro:content';
import { loader } from 'fumadocs-core/source';
import type * as PageTree from 'fumadocs-core/page-tree';
import { toFumadocsSource } from '@fumadocs/astro/source';

export const source = loader({
	source: toFumadocsSource(await getCollection('docs'), await getCollection('meta')),
	baseUrl: '/',
});

/** A route per docs page: `[...slug].astro` renders it, `[...slug].md.ts` writes its Markdown. */
export const pagePaths = () => source.getPages().map((page) => ({ params: { slug: page.slugs.join('/') }, props: { page } }));

/**
 * A page's link as the site has always served it, with a trailing slash (gh-pages serves /a/b/index.html at /a/b/).
 * Fumadocs drops the slash from its URLs, so the sidebar, the previous and next links and search results add it back.
 */
export function withSlash(url: string): string {
	if (!url.startsWith('/')) return url;
	const [path, hash] = url.split('#');
	const slashed = path.endsWith('/') ? path : `${path}/`;
	return hash === undefined ? slashed : `${slashed}#${hash}`;
}

/**
 * The sidebar, with a page's `sidebar.label` in its frontmatter in place of its title where it has one. The folder of
 * the `current` page starts open: Fumadocs cannot find it, since the tree's URLs end with a slash and it drops the slash
 * from the page's address before it compares them.
 */
export function sidebarTree(current?: string): PageTree.Root {
	const tree = source.getPageTree();
	const labels = new Map(
		source
			.getPages()
			.filter((p) => p.data.sidebar?.label)
			.map((p) => [p.url, p.data.sidebar!.label!]),
	);
	const relabel = (nodes: PageTree.Node[]): PageTree.Node[] =>
		nodes.map((node) => {
			if (node.type === 'page') return { ...node, url: withSlash(node.url), name: labels.get(node.url) ?? node.name };
			if (node.type !== 'folder') return node;
			const children = relabel(node.children);
			return { ...node, children, defaultOpen: children.some((c) => c.type === 'page' && c.url === `${current}/`) };
		});
	return { ...tree, children: relabel(tree.children) };
}

/**
 * The page's `JOB nn` banner (docs/DESIGN.md, "Banner"): nn is the page's position under its sidebar heading or folder,
 * and the label is that heading or folder. A page under neither is `DOCS`.
 */
export function banner(url: string): { id: string; label: string } {
	const find = (nodes: PageTree.Node[], heading: string): { id: string; label: string } | undefined => {
		let label = heading;
		let n = 0;
		for (const node of nodes) {
			if (node.type === 'separator') {
				label = String(node.name ?? '');
				n = 0;
			} else if (node.type === 'page') {
				n++;
				if (node.url === url) return { id: `JOB ${String(n).padStart(2, '0')}`, label };
			} else {
				const found = find(node.children, String(node.name ?? ''));
				if (found) return found;
			}
		}
	};
	return find(source.getPageTree().children, '') ?? { id: 'DOCS', label: '' };
}
