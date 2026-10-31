// Feature matrix: search, group, flags and library columns. All filters combine with AND.

const root = document.querySelector<HTMLElement>('[data-mx]');
if (root) init(root);

function init(root: HTMLElement) {
	const q = root.querySelector<HTMLInputElement>('[data-q]')!;
	const rows = Array.from(root.querySelectorAll<HTMLElement>('[data-body] tr'));
	const count = root.querySelector<HTMLElement>('[data-count]')!;
	const empty = root.querySelector<HTMLElement>('[data-empty]')!;

	let group = '';
	const flags = new Set<string>();
	const libs = new Set<string>(['varmq']);

	const chips = Array.from(root.querySelectorAll<HTMLElement>('[data-lib-chip]'));
	// A narrow column shows varmq and two others. The reader can add the rest.
	const narrow = root.clientWidth < 640;
	chips.forEach((c, i) => {
		const on = !narrow || i < 2;
		c.setAttribute('aria-pressed', String(on));
		if (on) libs.add(c.dataset.libChip!);
	});

	function apply() {
		const text = q.value.trim().toLowerCase();
		let shown = 0;
		for (const r of rows) {
			const ok =
				(!group || r.dataset.group === group) &&
				(!text || r.dataset.text!.includes(text)) &&
				(!flags.has('limit') || r.dataset.limit === 'true') &&
				(!flags.has('shared') || r.dataset.shared === 'true') &&
				(!flags.has('unique') || r.dataset.shared === 'false');
			r.hidden = !ok;
			if (ok) shown++;
		}
		for (const el of root.querySelectorAll<HTMLElement>('[data-lib]')) el.hidden = !libs.has(el.dataset.lib!);
		count.textContent = `Showing ${shown} of ${rows.length} rows, ${libs.size} of 7 libraries.`;
		empty.hidden = shown > 0;
		root.querySelector('table')!.hidden = shown === 0;
	}

	root.addEventListener('click', (e) => {
		const t = e.target as HTMLElement;
		const g = t.closest<HTMLElement>('[data-group].mx__chip');
		const f = t.closest<HTMLElement>('[data-flag]');
		const l = t.closest<HTMLElement>('[data-lib-chip]');
		if (g) {
			group = g.dataset.group ?? '';
			root.querySelectorAll<HTMLElement>('[data-group].mx__chip').forEach((b) => b.setAttribute('aria-pressed', String(b === g)));
		} else if (f) {
			const k = f.dataset.flag!;
			// "Others have it too" and "Only varmq" cannot both hold.
			if (flags.has(k)) flags.delete(k);
			else {
				flags.add(k);
				if (k === 'shared') flags.delete('unique');
				if (k === 'unique') flags.delete('shared');
			}
			root.querySelectorAll<HTMLElement>('[data-flag]').forEach((b) => b.setAttribute('aria-pressed', String(flags.has(b.dataset.flag!))));
		} else if (l) {
			const k = l.dataset.libChip!;
			if (libs.has(k)) libs.delete(k);
			else libs.add(k);
			l.setAttribute('aria-pressed', String(libs.has(k)));
		} else if (t.closest('[data-clear]')) {
			group = '';
			flags.clear();
			q.value = '';
			root.querySelectorAll<HTMLElement>('[data-group].mx__chip').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.group === '')));
			root.querySelectorAll<HTMLElement>('[data-flag]').forEach((b) => b.setAttribute('aria-pressed', 'false'));
		} else return;
		apply();
	});
	q.addEventListener('input', apply);
	apply();
}
