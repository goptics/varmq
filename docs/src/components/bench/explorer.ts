// Browser side of the benchmark explorer: the dataset tabs, the filter selects, the win map and the copy button.
// The markup of the results comes from render.ts, the same code the build ran for the default state.

import raw from './data.json';
import { VIEWS, activeFilters, defaultState, filterDefs, fromHash, metricsFor, normalize, optionCounts, toHash, viewById } from './lib.ts';
import type { Data, State } from './lib.ts';
import { renderAll } from './render.ts';
import type { Out } from './render.ts';
import { copyButtons, tabs as tabsOf } from '../ui/dom';

const data = raw as unknown as Data;
const root = document.querySelector<HTMLElement>('[data-bench]');

if (root) boot(root);
copyButtons();

function boot(root: HTMLElement) {
	const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');
	const tabs = Array.from(root.querySelectorAll<HTMLElement>('[role="tab"]'));
	const states = new Map<string, State>(VIEWS.map((v) => [v.id, defaultState(data, v.id)]));
	const last = new Map<string, Partial<Out>>();
	let active = VIEWS[0].id;

	const panel = (id: string) => root.querySelector<HTMLElement>(`[data-view="${id}"]`)!;
	const out = (p: HTMLElement, key: string) => p.querySelector<HTMLElement>(`[data-out="${key}"]`);

	root.querySelectorAll<HTMLElement>('[data-js]').forEach((el) => el.removeAttribute('hidden'));
	root.classList.add('is-live');

	// ---- state in the URL hash, so a view can be linked ----

	const writeHash = () => {
		const h = toHash(data, states.get(active)!);
		const url = location.pathname + location.search + (h ? `#${h}` : '');
		history.replaceState(null, '', url);
	};

	const readHash = () => {
		if (!/(^#|&)(view|metric|sort|baselines|focus|test|task|workers|shape|instances|scenario|ckpt)=/.test(location.hash)) return false;
		const s = fromHash(data, location.hash);
		states.set(s.view, s);
		active = s.view;
		return true;
	};

	// ---- rendering ----

	function update(viewId: string, opts: { animate?: boolean; user?: boolean } = {}) {
		const s = normalize(data, states.get(viewId)!);
		states.set(viewId, s);
		const p = panel(viewId);
		const next = renderAll(data, s);
		const prev = last.get(viewId) ?? renderAll(data, defaultState(data, viewId));
		for (const key of Object.keys(next) as (keyof Out)[]) {
			if (prev[key] === next[key]) continue;
			const el = out(p, key);
			if (el) el.innerHTML = next[key];
		}
		last.set(viewId, next);
		if (opts.animate && !reduce.matches && prev.lanes !== next.lanes) {
			const lanes = p.querySelector<HTMLElement>('.rlanes');
			lanes?.classList.add('is-enter');
		}
		const lossBox = p.querySelector<HTMLDetailsElement>('.bloss details');
		if (lossBox) lossBox.hidden = !next.lossesBody;
		p.querySelectorAll<HTMLElement>('[data-js]').forEach((el) => el.removeAttribute('hidden'));
		syncControls(p, s);
		markFocus(p, s);
		wireMap(p);
		if (opts.user && viewId === active) writeHash();
	}

	function syncControls(p: HTMLElement, s: State) {
		const view = viewById(s.view);
		const defs = filterDefs(data, view);
		const live = new Set(activeFilters(defs, s.f).map((d) => d.id));
		const counts = optionCounts(data, s);
		for (const fs of p.querySelectorAll<HTMLElement>('[data-filter]')) {
			const id = fs.dataset.filter!;
			fs.hidden = !live.has(id);
			const select = fs.querySelector('select')!;
			// An option that would leave no group is disabled, unless it is the current value.
			for (const o of select.options) o.disabled = counts[id][o.value] === 0 && o.value !== s.f[id];
			select.value = s.f[id];
		}
		const metrics = metricsFor(view.suite, s.f).map((m) => m.id);
		for (const label of p.querySelectorAll<HTMLElement>('[data-m]')) {
			label.hidden = !metrics.includes(label.dataset.m!);
			const input = label.querySelector('input')!;
			input.checked = input.value === s.metric;
		}
		const sort = p.querySelector<HTMLSelectElement>('[data-sort] select');
		if (sort) sort.value = s.sort;
		const base = p.querySelector<HTMLInputElement>('[data-baselines] input');
		if (base) base.checked = s.baselines;
		const def = defaultState(data, s.view);
		const clean = JSON.stringify(def.f) === JSON.stringify(s.f) && def.baselines === s.baselines && !s.focus;
		const reset = p.querySelector<HTMLButtonElement>('[data-reset]');
		if (reset) reset.disabled = clean;
	}

	function markFocus(p: HTMLElement, s: State) {
		for (const b of p.querySelectorAll<HTMLButtonElement>('.bcell')) b.setAttribute('aria-pressed', String(b.dataset.cell === s.focus));
	}

	// ---- the win map: a table of buttons with one tab stop and arrow-key movement ----

	function wireMap(p: HTMLElement) {
		const table = p.querySelector<HTMLTableElement>('.bmap__t');
		if (!table || table.dataset.wired) return;
		table.dataset.wired = '1';
		const read = p.querySelector<HTMLElement>('[data-read]');
		const idle = read?.textContent ?? '';
		const say = (b: HTMLElement | null) => {
			if (read) read.textContent = b ? (b.dataset.text ?? '') : idle;
		};
		table.addEventListener('pointerover', (e) => say((e.target as HTMLElement).closest<HTMLElement>('.bcell')));
		table.addEventListener('pointerleave', () => say(table.querySelector<HTMLElement>('.bcell:focus')));
		table.addEventListener('focusin', (e) => {
			const b = (e.target as HTMLElement).closest<HTMLElement>('.bcell');
			if (!b) return;
			for (const x of table.querySelectorAll<HTMLElement>('.bcell')) x.tabIndex = x === b ? 0 : -1;
			say(b);
		});
		table.addEventListener('focusout', () => say(null));
		table.addEventListener('keydown', (e) => {
			const b = (e.target as HTMLElement).closest<HTMLElement>('.bcell');
			if (!b) return;
			const rows = Array.from(table.tBodies[0].rows);
			const r = rows.indexOf(b.closest('tr')!);
			const cells = (row: HTMLTableRowElement) => Array.from(row.querySelectorAll<HTMLElement>('td')).map((td) => td.querySelector<HTMLElement>('.bcell'));
			const c = cells(rows[r]).indexOf(b);
			const step = (dr: number, dc: number) => {
				let rr = r + dr;
				let cc = c + dc;
				while (rr >= 0 && rr < rows.length) {
					const row = cells(rows[rr]);
					while (cc >= 0 && cc < row.length) {
						if (row[cc]) return row[cc];
						if (dc === 0) break;
						cc += dc;
					}
					if (dr === 0) return null;
					rr += dr;
					cc = c;
				}
				return null;
			};
			let to: HTMLElement | null | undefined;
			if (e.key === 'ArrowRight') to = step(0, 1);
			else if (e.key === 'ArrowLeft') to = step(0, -1);
			else if (e.key === 'ArrowDown') to = step(1, 0);
			else if (e.key === 'ArrowUp') to = step(-1, 0);
			else if (e.key === 'Home') to = cells(rows[r]).find(Boolean);
			else if (e.key === 'End') to = cells(rows[r]).filter(Boolean).at(-1);
			else return;
			e.preventDefault();
			to?.focus();
		});
	}

	// ---- tabs ----

	// The shared tabs set aria-selected and the roving tabindex; this shows the panel and redraws.
	const tabControl = tabsOf(root.querySelector<HTMLElement>('[role="tablist"]')!, (key) => {
		active = key;
		for (const t of tabs) document.getElementById(t.getAttribute('aria-controls') ?? '')?.classList.toggle('is-on', t.dataset.key === key);
		update(active, { user: true });
	});

	// ---- controls ----

	root.addEventListener('submit', (e) => e.preventDefault());

	root.addEventListener('change', (e) => {
		// A select (filters, order) or an input (show, baselines); both carry the new value.
		const input = e.target as HTMLInputElement | HTMLSelectElement;
		const p = input.closest<HTMLElement>('[data-view]');
		if (!p || !input.closest('[data-controls]')) return;
		const id = p.dataset.view!;
		const s = states.get(id)!;
		const fs = input.closest<HTMLElement>('[data-filter]');
		if (fs) s.f = { ...s.f, [fs.dataset.filter!]: input.value };
		else if (input.closest('[data-metric]')) s.metric = input.value;
		else if (input.closest('[data-sort]')) s.sort = input.value as State['sort'];
		else if (input.closest('[data-baselines]')) s.baselines = (input as HTMLInputElement).checked;
		if (fs || input.closest('[data-baselines]')) s.focus = null;
		update(id, { animate: true, user: true });
	});

	root.addEventListener('click', async (e) => {
		const el = e.target as HTMLElement;
		const p = el.closest<HTMLElement>('[data-view]');
		const id = p?.dataset.view;

		const cell = el.closest<HTMLElement>('[data-cell], [data-focus]');
		if (cell && id) {
			const key = cell.dataset.cell ?? cell.dataset.focus!;
			const s = states.get(id)!;
			s.focus = s.focus === key && cell.dataset.cell ? null : key;
			update(id, { animate: true, user: true });
			if (cell.dataset.focus) p!.querySelector('[data-out="scope"]')?.scrollIntoView({ block: 'nearest', behavior: reduce.matches ? 'auto' : 'smooth' });
			return;
		}
		if (el.closest('[data-unfocus]') && id) {
			states.get(id)!.focus = null;
			update(id, { animate: true, user: true });
			return;
		}
		if (el.closest('[data-reset]') && id) {
			states.set(id, defaultState(data, id));
			update(id, { animate: true, user: true });
			return;
		}
	});

	// ---- start ----

	if (readHash()) {
		const t = tabs.find((x) => x.dataset.key === active);
		if (t) {
			for (const x of tabs) {
				x.setAttribute('aria-selected', String(x === t));
				x.tabIndex = x === t ? 0 : -1;
				document.getElementById(x.getAttribute('aria-controls') ?? '')?.classList.toggle('is-on', x === t);
			}
		}
	}
	update(active);
	window.addEventListener('hashchange', () => {
		if (readHash()) {
			tabControl.select(active);
		}
	});
}
