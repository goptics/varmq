// Shared client helpers for the learn scenes: motion preference, option groups, copy buttons.

import { highlightGo } from './go';

export const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

export const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/** Put a Go call into a `.lcall` element, highlighted. Several lines are allowed. */
export function setCall(el: Element | null | undefined, text: string) {
	if (el) el.innerHTML = highlightGo(text);
}

/**
 * Roving focus for a segmented control. Works for `role="tab"` (with panels) and `role="radio"`
 * (a plain choice). Arrow keys, Home and End move the choice; `onSelect` runs on every change.
 */
export function segmented(list: HTMLElement, onSelect: (item: HTMLElement) => void) {
	const items = () => Array.from(list.querySelectorAll<HTMLElement>('[role="tab"], [role="radio"]'));
	const isTab = items()[0]?.getAttribute('role') === 'tab';
	const attr = isTab ? 'aria-selected' : 'aria-checked';

	const select = (item: HTMLElement, focus = false) => {
		for (const it of items()) {
			const on = it === item;
			it.setAttribute(attr, String(on));
			it.tabIndex = on ? 0 : -1;
			if (isTab) {
				const panel = document.getElementById(it.getAttribute('aria-controls') ?? '');
				if (panel) panel.hidden = !on;
			}
		}
		if (focus) item.focus();
		onSelect(item);
	};

	list.addEventListener('click', (e) => {
		const it = (e.target as HTMLElement).closest<HTMLElement>('[role="tab"], [role="radio"]');
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

	return { select };
}

/** Wire every `[data-copy]` button in `root`: it copies the text of the element its selector names. */
export function copyButtons(root: ParentNode) {
	root.querySelectorAll<HTMLElement>('[data-copy]').forEach((btn) => {
		const label = btn.textContent ?? 'Copy';
		btn.addEventListener('click', async () => {
			const source = document.querySelector(btn.dataset.copy ?? '');
			const text = source?.textContent?.trim() ?? '';
			try {
				await navigator.clipboard.writeText(text);
				btn.textContent = 'Copied';
			} catch {
				btn.textContent = 'Select';
				if (source) {
					const range = document.createRange();
					range.selectNodeContents(source);
					const sel = window.getSelection();
					sel?.removeAllRanges();
					sel?.addRange(range);
				}
			}
			setTimeout(() => {
				btn.textContent = label;
			}, 1600);
		});
	});
}

/** True when a key press belongs to a text field, a select, or a segmented control. */
export function keyBelongsToControl(e: KeyboardEvent) {
	const t = e.target as HTMLElement;
	return !!t.closest('input, textarea, select, [role="tablist"], [role="radiogroup"]');
}
