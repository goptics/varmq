// Shared client helpers for the ops scenes: a tiny element builder, tabs, copy buttons and the
// reduced-motion check. Each scene imports what it needs, so a page ships only the code it uses.

export const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

type Child = Node | string | null | undefined | false;
type Attrs = Record<string, string | number | boolean | null | undefined>;

/** Build an element. `class` and `text` are plain attributes; `true` sets a boolean attribute. */
export function h<K extends keyof HTMLElementTagNameMap>(tag: K, attrs: Attrs = {}, ...kids: Child[]): HTMLElementTagNameMap[K] {
	const el = document.createElement(tag);
	for (const [k, v] of Object.entries(attrs)) {
		if (v === null || v === undefined || v === false) continue;
		if (k === 'text') el.textContent = String(v);
		else el.setAttribute(k, v === true ? '' : String(v));
	}
	for (const kid of kids) {
		if (kid === null || kid === undefined || kid === false) continue;
		el.append(kid instanceof Node ? kid : document.createTextNode(kid));
	}
	return el;
}

/** A job square, the same shape as the landing page's. */
export const job = (id: string | number, cls = '') => h('span', { class: `ops-job ${cls}`.trim(), text: String(id) });

/** Roving-tabindex tabs with arrow keys, Home and End. Calls `on` with the tab's `data-key`. */
export function tabs(list: HTMLElement, on?: (key: string, tab: HTMLElement) => void, opts: { shared?: HTMLElement } = {}) {
	const all = Array.from(list.querySelectorAll<HTMLElement>('[role="tab"]'));
	const select = (tab: HTMLElement, focus = false) => {
		for (const t of all) {
			const active = t === tab;
			t.setAttribute('aria-selected', String(active));
			t.tabIndex = active ? 0 : -1;
			// One shared panel (a scene) stays visible and points at the active tab. Separate panels swap.
			if (opts.shared) {
				if (active) opts.shared.setAttribute('aria-labelledby', t.id);
				continue;
			}
			const panel = document.getElementById(t.getAttribute('aria-controls') ?? '');
			if (panel) panel.hidden = !active;
		}
		if (focus) tab.focus();
		on?.(tab.dataset.key ?? '', tab);
	};
	list.addEventListener('click', (e) => {
		const tab = (e.target as HTMLElement).closest<HTMLElement>('[role="tab"]');
		if (tab && list.contains(tab)) select(tab);
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
			select(all[next], true);
		}
	});
	return { select: (key: string) => { const t = all.find((x) => x.dataset.key === key); if (t) select(t); } };
}

/** Copy buttons: `[data-ops-copy]` copies the text of the nearest `.ops-code pre`. */
let copyBound = false;
export function copyButtons() {
	if (copyBound) return;
	copyBound = true;
	document.addEventListener('click', async (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-ops-copy]');
		if (!btn) return;
		const card = btn.closest('.ops-code');
		const pre = card?.querySelector('pre');
		const live = card?.querySelector<HTMLElement>('[data-ops-live]');
		const label = btn.dataset.label ?? btn.textContent ?? 'Copy';
		btn.dataset.label = label;
		try {
			await navigator.clipboard.writeText(pre?.textContent?.replace(/\n$/, '') ?? '');
			btn.textContent = 'Copied';
			if (live) live.textContent = 'Copied to the clipboard.';
		} catch {
			btn.textContent = 'Select';
			if (pre) {
				const range = document.createRange();
				range.selectNodeContents(pre);
				const sel = window.getSelection();
				sel?.removeAllRanges();
				sel?.addRange(range);
			}
		}
		window.setTimeout(() => {
			btn.textContent = label;
			if (live) live.textContent = '';
		}, 1600);
	});
}

/** Run `fn` once the element is first in view. */
export function whenVisible(el: Element, fn: () => void) {
	const io = new IntersectionObserver((entries) => {
		if (entries.some((e) => e.isIntersecting)) {
			io.disconnect();
			fn();
		}
	});
	io.observe(el);
}
