import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { LABEL, codeTheme } from './code-theme.ts';

// WCAG relative luminance and contrast ratio.
const luminance = (hex: string) => {
	const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
	const lin = (c: number) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
	return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
};
const contrast = (a: string, b: string) => {
	const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
	return (hi + 0.05) / (lo + 0.05);
};

describe('code theme', () => {
	it('keeps every token colour readable on the label', () => {
		const colours = [codeTheme.colors!['editor.foreground']!, ...codeTheme.tokenColors!.map((t) => t.settings.foreground!)];
		for (const c of colours) assert.ok(contrast(c, LABEL) >= 4.5, `${c} on ${LABEL}: ${contrast(c, LABEL).toFixed(2)}`);
	});
});
