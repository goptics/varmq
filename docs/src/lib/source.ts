import { getCollection } from 'astro:content';
import { loader } from 'fumadocs-core/source';
import type * as PageTree from 'fumadocs-core/page-tree';
import { toFumadocsSource } from '@fumadocs/astro/source';

export const source = loader({
	source: toFumadocsSource(await getCollection('docs'), await getCollection('meta')),
	baseUrl: '/',
});

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

/** The sidebar, with a page's `sidebar.label` in its frontmatter in place of its title where it has one. */
export function sidebarTree(): PageTree.Root {
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
			if (node.type === 'folder') return { ...node, children: relabel(node.children) };
			return node;
		});
	return { ...tree, children: relabel(tree.children) };
}

/**
 * The page's `JOB nn` banner (docs/DESIGN.md, "Banner"): nn is the page's position under its sidebar heading, and the
 * label is the heading. A page under no heading is `DOCS`.
 */
export function banner(url: string): { id: string; label: string } {
	let label = '';
	let n = 0;
	for (const node of source.getPageTree().children) {
		if (node.type === 'separator') {
			label = String(node.name ?? '');
			n = 0;
		} else if (node.type === 'page') {
			n++;
			if (node.url === url) return { id: `JOB ${String(n).padStart(2, '0')}`, label };
		}
	}
	return { id: 'DOCS', label: '' };
}
