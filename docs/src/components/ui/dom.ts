// Client helpers that every scene and card shares: motion preference, an element builder, segmented
// tabs and radios, copy buttons and timers. A scene imports what it needs, so a page ships only that code.

import { highlightGo } from './go';

export const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

export const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

type Child = Node | string | null | undefined | false;
type Attrs = Record<string, string | number | boolean | Record<string, string> | null | undefined>;

/**
 * Build an element. `class` and `text` are plain attributes, `k` sets `data-k`, `attrs` holds more
 * attributes, and `true` sets a boolean attribute.
 */
export function h<K extends keyof HTMLElementTagNameMap>(tag: K, attrs: Attrs = {}, ...kids: Child[]): HTMLElementTagNameMap[K] {
	const el = document.createElement(tag);
	for (const [k, v] of Object.entries(attrs)) {
		if (v === null || v === undefined || v === false) continue;
		if (k === 'text') el.textContent = String(v);
		else if (k === 'k') el.dataset.k = String(v);
		else if (k === 'attrs' && typeof v === 'object') for (const [a, b] of Object.entries(v)) el.setAttribute(a, b);
		else el.setAttribute(k, v === true ? '' : String(v));
	}
	for (const kid of kids) {
		if (kid === null || kid === undefined || kid === false) continue;
		el.append(kid instanceof Node ? kid : document.createTextNode(kid));
	}
	return el;
}

/** Put a Go call into an element, highlighted. Several lines are allowed. */
export function setCall(el: Element | null | undefined, text: string) {
	if (el) el.innerHTML = highlightGo(text);
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
 * Segmented tabs (`role="tab"`, with panels) or radios (`role="radio"`, a plain choice). Roving tabindex;
 * arrow keys, Home and End move the choice. `on` runs on every change with the item's `data-key`.
 * `opts.shared` is one panel that stays visible and points at the active tab.
 */
export function tabs(list: HTMLElement, on?: (key: string, item: HTMLElement) => void, opts: { shared?: HTMLElement } = {}) {
	const sel = '[role="tab"], [role="radio"]';
	const items = () => Array.from(list.querySelectorAll<HTMLElement>(sel));
	const isTab = items()[0]?.getAttribute('role') === 'tab';
	const attr = isTab ? 'aria-selected' : 'aria-checked';

	const select = (item: HTMLElement, focus = false) => {
		for (const it of items()) {
			const active = it === item;
			it.setAttribute(attr, String(active));
			it.tabIndex = active ? 0 : -1;
			if (!isTab) continue;
			if (opts.shared) {
				if (active && it.id) opts.shared.setAttribute('aria-labelledby', it.id);
				continue;
			}
			const panel = document.getElementById(it.getAttribute('aria-controls') ?? '');
			if (panel) panel.hidden = !active;
		}
		if (focus) item.focus();
		on?.(item.dataset.key ?? '', item);
	};

	list.addEventListener('click', (e) => {
		const it = (e.target as HTMLElement).closest<HTMLElement>(sel);
		if (it && list.contains(it)) select(it);
	});

	list.addEventListener('keydown', (e) => {
		const all = items();
		const i = all.indexOf(document.activeElement as HTMLElement);
		if (i < 0) return;
		let next = -1;
		if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (i + 1) % all.length;
		else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (i - 1 + all.length) % all.length;
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = all.length - 1;
		if (next >= 0) {
			e.preventDefault();
			e.stopPropagation(); // a scene that listens for arrows must not also step
			select(all[next], true);
		}
	});

	return { select: (key: string) => { const it = items().find((x) => x.dataset.key === key); if (it) select(it); } };
}

/** True when a key press belongs to a text field, a select, or a segmented control. */
export function keyBelongsToControl(e: KeyboardEvent) {
	const t = e.target as HTMLElement;
	return !!t.closest('input, textarea, select, [role="tablist"], [role="radiogroup"]');
}

/**
 * Copy `text` and say so on the button for 1.6 s: "Copied", or "Select" when the browser refuses
 * (then `fallback` is selected so the reader can copy it by hand). `live` gets the same message.
 */
export async function copyText(btn: HTMLElement, text: string, fallback?: Element | null, live?: HTMLElement | null) {
	const label = btn.dataset.label ?? btn.textContent ?? 'Copy';
	btn.dataset.label = label;
	try {
		await navigator.clipboard.writeText(text);
		btn.textContent = 'Copied';
		if (live) live.textContent = 'Copied to the clipboard.';
	} catch {
		btn.textContent = 'Select';
		if (fallback) {
			const range = document.createRange();
			range.selectNodeContents(fallback);
			const sel = window.getSelection();
			sel?.removeAllRanges();
			sel?.addRange(range);
		}
	}
	window.setTimeout(() => {
		btn.textContent = label;
		if (live) live.textContent = '';
	}, 1600);
}

/**
 * Copy buttons. A `[data-copy]` button copies the text of the element its value names (a selector),
 * or, when the value is empty, the `pre` of its code card. A `[data-live]` element in the card or
 * `#live` on the page gets the message. One handler serves the whole document.
 */
let copyBound = false;
export function copyButtons() {
	if (copyBound) return;
	copyBound = true;
	document.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLElement>('[data-copy]');
		if (!btn) return;
		const card = btn.closest('.code');
		const source = btn.dataset.copy ? document.querySelector(btn.dataset.copy) : card?.querySelector('pre');
		const live = card?.querySelector<HTMLElement>('[data-live]') ?? document.getElementById('live');
		// innerText keeps the line breaks of block lines (a command in several `span`s).
		const text = ((source as HTMLElement | null)?.innerText ?? source?.textContent ?? '').replace(/\n$/, '').trim();
		void copyText(btn, text, source, live);
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
