// Helpers that only the guide scenes use: job chips, FLIP motion, a number stepper, the live region,
// the call strip and highlighted code lines. The scenes draw one working model each (see model.ts).
// DOM building, tabs, timers and motion preference come from the shared layer (../ui/dom).

import { h, reducedMotion, setCall } from '../ui/dom';
import { highlightGo } from '../ui/go';

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
	if (reducedMotion()) {
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

/** Writes to the polite live region of the scene. */
export function say(root: HTMLElement, text: string): void {
	const live = root.querySelector<HTMLElement>('[data-live]');
	if (live) live.textContent = text;
}

/** Writes the Go call that the last control made into the call strip of the scene. */
export function caption(root: HTMLElement, go: string): void {
	setCall(root.querySelector('[data-call]'), go);
}

/** Highlighted code with one `<span class="ga-line">` for each line, so a scene can mark the line that runs. */
export function codeLines(src: string): string {
	return src
		.split('\n')
		.map((l, i) => `<span class="ga-line" data-line="${i}">${highlightGo(l) || ' '}</span>`)
		.join('');
}
