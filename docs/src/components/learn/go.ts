// A small Go highlighter for the calls shown under each scene. It returns HTML with token classes;
// learn.css sets the colors from the shared tokens.

const KEYWORDS = new Set(
	'break default func interface select case defer go map struct chan else goto package switch const fallthrough if range type continue for import return var'.split(
		' ',
	),
);
const PREDECLARED = new Set(
	'true false nil iota any bool byte rune string error int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 float32 float64 make new len cap append panic'.split(
		' ',
	),
);

const TOKEN =
	/(\/\/[^\n]*|\/\*[\s\S]*?\*\/)|("(?:\\.|[^"\\\n])*"|`[^`]*`|'(?:\\.|[^'\\\n])+')|(\b\d[\d_]*(?:\.\d+)?\b)|([A-Za-z_]\w*)/g;

export const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

const wrap = (cls: string, text: string) => `<span class="ltk-${cls}">${escapeHtml(text)}</span>`;

export function highlightGo(src: string): string {
	let out = '';
	let last = 0;
	for (const m of src.matchAll(TOKEN)) {
		const index = m.index ?? 0;
		out += escapeHtml(src.slice(last, index));
		last = index + m[0].length;
		const [text, comment, str, num, ident] = m;
		if (comment) out += wrap('c', text);
		else if (str) out += wrap('s', text);
		else if (num) out += wrap('n', text);
		else if (ident) {
			const next = src[last];
			const prev = src[index - 1];
			if (KEYWORDS.has(ident)) out += wrap('k', ident);
			else if (PREDECLARED.has(ident)) out += wrap('b', ident);
			else if (next === '(') out += wrap('f', ident);
			else if (prev === '.' && /^[A-Z]/.test(ident)) out += wrap('t', ident);
			else out += escapeHtml(ident);
		}
	}
	return out + escapeHtml(src.slice(last));
}
