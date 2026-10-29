import type { ThemeRegistration } from 'shiki';

// Go code on the cream station label: soot for most text, and the darker inks of the page's world for the rest,
// each 4.5:1 or more against the label (theme.test.ts measures them).
export const LABEL = '#efe6cf';

export const codeTheme: ThemeRegistration = {
	name: 'tube-label',
	type: 'light',
	colors: { 'editor.background': LABEL, 'editor.foreground': '#1d1912' },
	tokenColors: [
		{ scope: ['comment', 'punctuation.definition.comment'], settings: { foreground: '#5a5243' } },
		{ scope: ['keyword', 'storage', 'storage.type', 'keyword.operator'], settings: { foreground: '#17493a' } },
		{ scope: ['string', 'punctuation.definition.string'], settings: { foreground: '#6b4a12' } },
		{ scope: ['constant.numeric', 'constant.language', 'constant.other'], settings: { foreground: '#8f2b1d' } },
		{ scope: ['entity.name.type', 'support.type'], settings: { foreground: '#17493a' } },
		{ scope: ['entity.name.function', 'support.function', 'variable', 'punctuation'], settings: { foreground: '#1d1912' } },
	],
};
