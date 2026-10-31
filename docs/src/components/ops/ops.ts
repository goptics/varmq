// A job square and a focus helper for the ops scenes. Everything else they need comes from `../ui/dom`.

import { h } from '../ui/dom';

/** A job square, the same shape as the landing page's. */
export const job = (id: string | number, cls = '') => h('span', { class: `ops-job ${cls}`.trim(), text: String(id) });

/**
 * After a render, if the control just pressed is now hidden or disabled, give focus to the first control
 * of the scene that can still be used, so a keyboard reader does not land on the page body. Call it only
 * for a press from the keyboard: read `el.matches(':focus-visible')` before the action hides the control.
 */
export function refocus(figure: HTMLElement, pressed: HTMLElement) {
	const gone = (el: HTMLElement) => (el as HTMLButtonElement).disabled || el.getClientRects().length === 0;
	if (!gone(pressed)) return;
	Array.from(figure.querySelectorAll<HTMLElement>('[data-act], input'))
		.find((el) => !gone(el))
		?.focus();
}
