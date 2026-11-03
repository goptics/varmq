// Highlighters for the code shown in cards and scenes. Each returns HTML with `tk-*` token classes.
// `ui.css` sets the colors from the shared tokens, and pins them to the dark values inside a panel.

export const escapeHtml = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

const wrap = (cls: string, text: string) => `<span class="tk-${cls}">${escapeHtml(text)}</span>`;

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

/** Shell lines: comments, quoted strings, the command word and its flags. */
export function highlightShell(src: string): string {
	return src
		.split('\n')
		.map((line) => {
			if (/^\s*#/.test(line)) return wrap('c', line);
			let out = '';
			let first = true;
			for (const part of line.split(/(\s+|"[^"]*"|'[^']*')/)) {
				if (part === '') continue;
				if (/^\s+$/.test(part)) out += part;
				else if (/^["']/.test(part)) out += wrap('s', part);
				else if (first) {
					out += wrap('f', part);
					first = false;
				} else if (/^--?[A-Za-z]/.test(part)) out += wrap('b', part);
				else out += escapeHtml(part);
			}
			return out;
		})
		.join('\n');
}

/** JSON: keys, strings, numbers and literals. */
export function highlightJson(src: string): string {
	return escapeHtml(src).replace(
		/(&quot;|")((?:\\.|[^"\\])*?)\1(\s*:)?|\b(true|false|null)\b|(-?\b\d+(?:\.\d+)?\b)/g,
		(m, _q, _body, colon, lit, num) => {
			if (lit) return `<span class="tk-k">${lit}</span>`;
			if (num) return `<span class="tk-n">${num}</span>`;
			return colon ? `<span class="tk-t">${m.slice(0, m.length - colon.length)}</span>${colon}` : `<span class="tk-s">${m}</span>`;
		},
	);
}
