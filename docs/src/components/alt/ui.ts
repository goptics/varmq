// Behavior for the landing page: tabs, copy buttons (both from the shared helpers) and the status bar.

import { tabs, copyButtons } from '../ui/dom';

document.querySelectorAll<HTMLElement>('[role="tablist"]').forEach((list) => {
	tabs(list, (key, tab) => tab.dispatchEvent(new CustomEvent('tabchange', { bubbles: true, detail: { key } })));
});

copyButtons();

// Status bar mirrors the hero pool.
const bar = document.querySelector<HTMLElement>('[data-bar]');
if (bar) {
	const set = (k: string, v: string) => {
		const el = bar.querySelector<HTMLElement>(`[data-bar-${k}]`);
		if (el) el.textContent = v;
	};
	document.addEventListener('varmq:pool', (e) => {
		const d = (e as CustomEvent<{ workers: number; waiting: number; state: string; done: number }>).detail;
		set('workers', String(d.workers));
		set('waiting', String(d.waiting));
		set('done', String(d.done));
		set('state', d.state);
		const dot = bar.querySelector<HTMLElement>('[data-bar-dot]');
		if (dot) dot.dataset.s = d.state;
	});
}
