// Small helpers that every guide scene shares: DOM building, FLIP motion, tabs, steppers, the
// live region and a Go highlighter. The scenes draw one working model each (see model.ts).

export const reduced = (): boolean => matchMedia('(prefers-reduced-motion: reduce)').matches;

type Kid = Node | string | null | undefined | false;

export function h<K extends keyof HTMLElementTagNameMap>(
	tag: K,
	props: { class?: string; text?: string; attrs?: Record<string, string>; k?: string | number } = {},
	...kids: Kid[]
): HTMLElementTagNameMap[K] {
	const el = document.createElement(tag);
	if (props.class) el.className = props.class;
	if (props.text !== undefined) el.textContent = props.text;
	if (props.k !== undefined) el.dataset.k = String(props.k);
	for (const [k, v] of Object.entries(props.attrs ?? {})) el.setAttribute(k, v);
	for (const kid of kids) if (kid) el.append(kid);
	return el;
}

export function q<T extends HTMLElement>(root: ParentNode, sel: string): T {
	const el = root.querySelector<T>(sel);
	if (!el) throw new Error(`guides-a: missing ${sel}`);
	return el;
}

/** A job chip. `tone` picks the color; the label always carries the meaning too. */
export function chip(label: string, tone: '' | 'amber' | 'cobalt' | 'danger' = '', k?: string | number): HTMLElement {
	return h('span', { class: `chip${tone ? ` chip--${tone}` : ''}`, text: label, k, attrs: { role: 'listitem' } });
}

/**
 * Moves elements that carry `data-k` with a FLIP animation. `mutate` changes the DOM. A new element
 * fades in. With reduced motion the DOM changes and nothing moves.
 */
export function flip(root: HTMLElement, mutate: () => void, ms = 460): void {
	if (reduced()) {
		mutate();
		return;
	}
	const before = new Map<string, DOMRect>();
	root.querySelectorAll<HTMLElement>('[data-k]').forEach((el) => before.set(el.dataset.k!, el.getBoundingClientRect()));
	mutate();
	const easing = getComputedStyle(root).getPropertyValue('--ease').trim() || 'ease-out';
	root.querySelectorAll<HTMLElement>('[data-k]').forEach((el) => {
		const was = before.get(el.dataset.k!);
		if (!was) {
			el.animate([{ opacity: 0, transform: 'scale(0.82)' }, { opacity: 1, transform: 'none' }], { duration: ms * 0.7, easing });
			return;
		}
		const now = el.getBoundingClientRect();
		const dx = was.left - now.left;
		const dy = was.top - now.top;
		if (dx || dy) el.animate([{ transform: `translate(${dx}px, ${dy}px)` }, { transform: 'none' }], { duration: ms, easing });
	});
}

/** Timeouts that one reset can cancel. */
export class Timers {
	private ids = new Set<number>();
	after(ms: number, fn: () => void): number {
		const id = window.setTimeout(() => {
			this.ids.delete(id);
			fn();
		}, ms);
		this.ids.add(id);
		return id;
	}
	clear(): void {
		this.ids.forEach((id) => clearTimeout(id));
		this.ids.clear();
	}
}

/**
 * Segmented tabs (`role="tablist"`) or a radio group (`role="radiogroup"`): arrow keys, Home and End, one
 * choice at a time. The choices carry `data-key`.
 */
export function tabs(list: HTMLElement, onSelect: (key: string) => void): (key: string) => void {
	const role = list.getAttribute('role') === 'radiogroup' ? 'radio' : 'tab';
	const attr = role === 'radio' ? 'aria-checked' : 'aria-selected';
	const all = Array.from(list.querySelectorAll<HTMLElement>(`[role="${role}"]`));
	const select = (key: string, focus = false) => {
		for (const t of all) {
			const on = t.dataset.key === key;
			t.setAttribute(attr, String(on));
			t.tabIndex = on ? 0 : -1;
			if (on && focus) t.focus();
		}
		onSelect(key);
	};
	list.addEventListener('click', (e) => {
		const t = (e.target as HTMLElement).closest<HTMLElement>(`[role="${role}"]`);
		if (t && list.contains(t)) select(t.dataset.key!);
	});
	list.addEventListener('keydown', (e) => {
		const i = all.indexOf(document.activeElement as HTMLElement);
		if (i < 0) return;
		let next = -1;
		if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (i + 1) % all.length;
		else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (i - 1 + all.length) % all.length;
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = all.length - 1;
		if (next >= 0) {
			e.preventDefault();
			select(all[next].dataset.key!, true);
		}
	});
	return select;
}

/** A number stepper: a minus button, a value, a plus button. */
export function stepper(
	root: HTMLElement,
	opts: { min: number; max: number; value: number; format?: (n: number) => string; onChange: (n: number) => void },
): { set: (n: number) => void; get: () => number } {
	const dec = q<HTMLButtonElement>(root, '[data-dec]');
	const inc = q<HTMLButtonElement>(root, '[data-inc]');
	const out = q<HTMLElement>(root, '[data-val]');
	let v = opts.value;
	const paint = () => {
		out.textContent = (opts.format ?? String)(v);
		dec.disabled = v <= opts.min;
		inc.disabled = v >= opts.max;
	};
	const set = (n: number) => {
		v = Math.min(opts.max, Math.max(opts.min, n));
		paint();
		opts.onChange(v);
	};
	dec.addEventListener('click', () => set(v - 1));
	inc.addEventListener('click', () => set(v + 1));
	paint();
	return { set, get: () => v };
}

/** Writes to the polite live region of the scene and, optionally, to the caption line. */
export function say(root: HTMLElement, text: string): void {
	const live = root.querySelector<HTMLElement>('[data-live]');
	if (live) live.textContent = text;
}

export function caption(root: HTMLElement, go: string): void {
	const cap = root.querySelector<HTMLElement>('[data-cap]');
	if (cap) cap.innerHTML = highlightGo(go);
}

// ---- a small Go highlighter. The classes are styled in guides-a.css. ----

const KEYWORDS = new Set(
	'break default func interface select case defer go map struct chan else goto package switch const fallthrough if range type continue for import return var'.split(' '),
);
const BUILTIN = new Set('true false nil iota any bool byte rune string error int float64 make new len cap append panic'.split(' '));
const TOKEN = /(\/\/[^\n]*)|("(?:\\.|[^"\\\n])*"|`[^`]*`)|(\b\d[\d_]*(?:\.\d+)?\b)|([A-Za-z_]\w*)/g;
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const span = (c: string, t: string) => `<span class="tk-${c}">${esc(t)}</span>`;

export function highlightGo(src: string): string {
	let out = '';
	let last = 0;
	for (const m of src.matchAll(TOKEN)) {
		const i = m.index ?? 0;
		out += esc(src.slice(last, i));
		last = i + m[0].length;
		const [text, comment, str, num, ident] = m;
		if (comment) out += span('c', text);
		else if (str) out += span('s', text);
		else if (num) out += span('n', text);
		else if (KEYWORDS.has(ident)) out += span('k', ident);
		else if (BUILTIN.has(ident)) out += span('b', ident);
		else if (src[last] === '(') out += span('f', ident);
		else if (src[i - 1] === '.' && /^[A-Z]/.test(ident)) out += span('t', ident);
		else out += esc(ident);
	}
	return out + esc(src.slice(last));
}

/** Highlighted code with one `<span class="ga-line">` for each line, so a scene can mark the line that runs. */
export function codeLines(src: string): string {
	return src
		.split('\n')
		.map((l, i) => `<span class="ga-line" data-line="${i}">${highlightGo(l) || ' '}</span>`)
		.join('');
}
