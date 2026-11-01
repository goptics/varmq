// `collapse={a-b}` in a code block's meta folds lines a to b (1-based, inclusive) into a <details>, so the block opens
// on the code that matters. The summary says how many lines it holds; opening it shows them in place. Several ranges
// can be given, comma separated: `collapse={1-5, 20-24}`.
import type { Element, ElementContent } from 'hast';
import type { ShikiTransformer } from 'shiki';

export function parseCollapse(meta: string | undefined): [number, number][] {
	const m = meta?.match(/collapse=\{([^}]*)\}/);
	if (!m) return [];
	return m[1]
		.split(',')
		.map((part) => part.trim().split('-').map(Number))
		.filter(([a, b]) => a > 0 && b >= a)
		.map(([a, b]) => [a, b] as [number, number]);
}

export function transformerCollapse(): ShikiTransformer {
	return {
		name: 'varmq:collapse',
		code(code) {
			const ranges = parseCollapse((this.options.meta as { __raw?: string } | undefined)?.__raw);
			if (ranges.length === 0) return;
			// Shiki puts a newline text node between lines; keep it with the line before it.
			const lines: ElementContent[][] = [];
			for (const child of code.children) {
				if (child.type === 'element') lines.push([child]);
				else if (lines.length > 0) lines[lines.length - 1].push(child);
				else lines.push([child]);
			}
			const out: ElementContent[] = [];
			let i = 0;
			for (const [a, b] of ranges.sort((x, y) => x[0] - y[0])) {
				while (i < a - 1 && i < lines.length) out.push(...lines[i++]);
				const folded: ElementContent[] = [];
				while (i < b && i < lines.length) folded.push(...lines[i++]);
				const n = b - a + 1;
				const details: Element = {
					type: 'element',
					tagName: 'details',
					properties: { className: ['collapse'] },
					children: [
						{
							type: 'element',
							tagName: 'summary',
							properties: {},
							children: [{ type: 'text', value: `${n} collapsed ${n === 1 ? 'line' : 'lines'}` }],
						},
						...folded,
					],
				};
				out.push(details);
			}
			while (i < lines.length) out.push(...lines[i++]);
			code.children = out;
		},
	};
}
