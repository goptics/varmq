// @ts-check
import { defineEcConfig } from '@astrojs/starlight/expressive-code';
import { pluginCollapsibleSections } from '@expressive-code/plugin-collapsible-sections';

// Code is a dark panel in both themes (docs/DESIGN.md, "Dark surfaces"), so there is one theme.
// Syntax colors are the CSS variables `--tk-*`. docs-theme.css pins them to their dark values inside
// `.expressive-code`. Expressive Code cannot read a CSS variable, so the hex values below are only the
// fallback it needs for its own math. The panel itself paints from `styleOverrides`.
const FALLBACK = {
	panel: '#0a1511',
	ink: '#e4f2ec',
};

/** @param {string} scope @param {string} token @param {string} [fontStyle] */
const rule = (scope, token, fontStyle) => ({
	scope,
	settings: { foreground: `var(--tk-${token})`, ...(fontStyle ? { fontStyle } : {}) },
});

/** @type {import('@astrojs/starlight/expressive-code').StarlightExpressiveCodeOptions['themes']} */
const themes = [
	{
		name: 'varmq-panel',
		type: 'dark',
		colors: {
			'editor.background': FALLBACK.panel,
			'editor.foreground': FALLBACK.ink,
		},
		settings: [
			{ settings: { foreground: 'var(--panel-ink)' } },
			rule('comment, punctuation.definition.comment', 'c', 'italic'),
			rule(
				'keyword.control, keyword.function, keyword.package, keyword.import, keyword.var, keyword.const, keyword.type, keyword.struct, keyword.interface, keyword.map, keyword.channel, storage.modifier',
				'k',
			),
			rule('string, punctuation.definition.string, constant.character', 's'),
			rule('constant.numeric', 'n'),
			rule('entity.name.function', 'f'),
			rule('entity.name.type', 't'),
			// More specific scopes win: a package name is plain text, and a builtin is a builtin, not a call.
			{ scope: 'entity.name.type.package', settings: { foreground: 'var(--panel-ink)' } },
			rule('entity.name.function.support.builtin, constant.language, storage.type', 'b'),
		],
	},
];

export default defineEcConfig({
	themes,
	useDarkModeMediaQuery: false,
	themeCssSelector: false,
	useThemedScrollbars: false,
	minSyntaxHighlightingColorContrast: 0,
	plugins: [pluginCollapsibleSections()],
	styleOverrides: {
		borderRadius: '12px',
		borderWidth: '1px',
		borderColor: 'var(--panel-rule)',
		codeBackground: 'var(--panel)',
		codeForeground: 'var(--panel-ink)',
		codeFontFamily: 'var(--mono)',
		codeFontSize: '0.8125rem',
		codeLineHeight: '1.65',
		codePaddingBlock: '1rem',
		codePaddingInline: '1.125rem',
		uiFontFamily: 'var(--mono)',
		uiFontSize: '0.75rem',
		uiLineHeight: '1.2',
		focusBorder: 'var(--panel-accent)',
		frames: {
			frameBoxShadowCssValue: 'none',
			editorBackground: 'var(--panel)',
			terminalBackground: 'var(--panel)',
			editorTabBarBackground: 'var(--panel-2)',
			editorActiveTabBackground: 'var(--panel-2)',
			editorActiveTabForeground: 'var(--panel-ink-2)',
			editorActiveTabBorderColor: 'transparent',
			editorActiveTabIndicatorTopColor: 'transparent',
			editorActiveTabIndicatorBottomColor: 'transparent',
			editorTabBarBorderBottomColor: 'var(--panel-rule)',
			terminalTitlebarBackground: 'var(--panel-2)',
			terminalTitlebarForeground: 'var(--panel-ink-2)',
			terminalTitlebarBorderBottomColor: 'var(--panel-rule)',
			terminalTitlebarDotsForeground: 'var(--panel-ink-2)',
			terminalTitlebarDotsOpacity: '0',
			inlineButtonForeground: 'var(--panel-ink)',
			inlineButtonBackground: 'var(--panel-ink)',
			inlineButtonBackgroundIdleOpacity: '0',
			inlineButtonBackgroundHoverOrFocusOpacity: '0.1',
			inlineButtonBackgroundActiveOpacity: '0.18',
			inlineButtonBorder: 'var(--panel-ink)',
			inlineButtonBorderOpacity: '0.22',
			tooltipSuccessBackground: 'var(--brand)',
			tooltipSuccessForeground: 'var(--brand-ink)',
		},
		collapsibleSections: {
			closedTextColor: 'var(--panel-ink-2)',
			closedBackgroundColor: 'var(--panel-2)',
			closedBorderColor: 'var(--panel-rule)',
			openBackgroundColorCollapsible: 'var(--panel-2)',
		},
		textMarkers: {
			markBackground: 'var(--brand)',
			markBorderColor: 'var(--brand)',
			insBackground: 'var(--brand)',
			insBorderColor: 'var(--brand)',
			delBackground: 'var(--amber)',
			delBorderColor: 'var(--amber)',
			backgroundOpacity: '0.18',
			borderOpacity: '0.7',
		},
	},
});
