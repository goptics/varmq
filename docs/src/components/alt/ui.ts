// Shared behavior: tabs, copy buttons and the status bar.

document.querySelectorAll<HTMLElement>('[role="tablist"]').forEach((list) => {
	const tabs = Array.from(list.querySelectorAll<HTMLElement>('[role="tab"]'));

	const select = (tab: HTMLElement, focus = false) => {
		for (const t of tabs) {
			const on = t === tab;
			t.setAttribute('aria-selected', String(on));
			t.tabIndex = on ? 0 : -1;
			const panel = document.getElementById(t.getAttribute('aria-controls') ?? '');
			if (panel) panel.hidden = !on;
		}
		if (focus) tab.focus();
		tab.dispatchEvent(new CustomEvent('tabchange', { bubbles: true, detail: { key: tab.dataset.key } }));
	};

	list.addEventListener('click', (e) => {
		const tab = (e.target as HTMLElement).closest<HTMLElement>('[role="tab"]');
		if (tab && list.contains(tab)) select(tab);
	});

	list.addEventListener('keydown', (e) => {
		const i = tabs.indexOf(document.activeElement as HTMLElement);
		if (i < 0) return;
		let next = -1;
		if (e.key === 'ArrowRight') next = (i + 1) % tabs.length;
		else if (e.key === 'ArrowLeft') next = (i - 1 + tabs.length) % tabs.length;
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = tabs.length - 1;
		if (next >= 0) {
			e.preventDefault();
			select(tabs[next], true);
		}
	});
});

const live = document.getElementById('alt-live');

document.addEventListener('click', async (e) => {
	const btn = (e.target as HTMLElement).closest<HTMLElement>('[data-copy]');
	if (!btn) return;
	const source = document.querySelector(btn.dataset.copy ?? '');
	const text = source?.textContent?.trim() ?? '';
	const label = btn.dataset.label ?? btn.textContent ?? 'Copy';
	btn.dataset.label = label;
	try {
		await navigator.clipboard.writeText(text);
		btn.textContent = 'Copied';
		if (live) live.textContent = 'Copied to the clipboard.';
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
	window.setTimeout(() => {
		btn.textContent = label;
	}, 1600);
});

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
