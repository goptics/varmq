import { codeToHtml } from 'shiki';
import { codeTheme } from './code-theme.ts';

/** Go code as HTML at build time: one `.line` per source line, each tagged with the captions it lights (`data-steps`). */
export function highlight(code: string, lines: number[][]): Promise<string> {
	return codeToHtml(code, {
		lang: 'go',
		theme: codeTheme,
		transformers: [
			{
				pre(node) {
					node.properties.class = 'hp-code';
					delete node.properties.style;
				},
				line(node, n) {
					const steps = lines.flatMap((on, i) => (on.includes(n) ? [i] : []));
					if (steps.length) node.properties['data-steps'] = steps.join(' ');
				},
			},
		],
	});
}
