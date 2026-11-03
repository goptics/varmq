// Interface map: the reader's adapter is a set of parts (Caps). Boxes show what that set satisfies.

import { BASE, BINDINGS, NODES, PRESETS, NO_CAPS, behaviours, type Caps, type NodeDef } from './adapter-data';
import { h, setCall } from '../ui/dom';

const root = document.querySelector<HTMLElement>('[data-amap]');
if (root) init(root);

function init(root: HTMLElement) {
	const figure = root.closest<HTMLElement>('.sframe')!;
	const detail = root.querySelector<HTMLElement>('[data-detail]')!;
	const bindList = root.querySelector<HTMLElement>('[data-bind]')!;
	const does = root.querySelector<HTMLElement>('[data-does]')!;
	const call = figure.querySelector<HTMLElement>('[data-call]')!;
	const nodes = new Map<string, HTMLButtonElement>();
	root.querySelectorAll<HTMLButtonElement>('[data-node]').forEach((b) => nodes.set(b.dataset.node!, b));
	const defs = new Map<string, NodeDef>([[BASE.id, BASE], ...NODES.map((n) => [n.id, n] as [string, NodeDef])]);

	let caps: Caps = { ...PRESETS[0].caps };
	let selected = 'IPersistentQueue';

	function bestCall(c: Caps) {
		const best = [...BINDINGS].reverse().find((b) => b.test(c) && b.name.startsWith('With'));
		return best ? `worker.${best.name}(${best.name.includes('Distributed') ? 'dq' : 'pq'})` : 'worker.With…(pq)   // pick the parts your type has';
	}

	function render() {
		for (const [id, btn] of nodes) {
			const def = defs.get(id)!;
			const on = def.on(caps);
			btn.dataset.on = String(on);
			btn.setAttribute('aria-current', String(id === selected));
			const pill = btn.querySelector<HTMLElement>('[data-pill]')!;
			if (def.kind !== 'base') pill.textContent = on ? 'yes' : 'no';
			btn.setAttribute('aria-label', `${id}: ${def.kind === 'base' ? 'every queue has it' : on ? 'your type implements it' : 'your type does not implement it'}`);
		}

		// Detail of the selected box.
		const def = defs.get(selected)!;
		const on = def.on(caps);
		const kids: (Node | null)[] = [
			h('p', { class: 'amap__h' }, h('code', { text: def.id })),
			h('p', { class: 'amap__p', text: def.adds }),
			def.methods.length ? h('ul', { class: 'amap__methods' }, ...def.methods.map((m) => h('li', {}, h('code', { text: m })))) : null,
			h('p', { class: 'amap__h2', text: 'The worker' }),
			h('ul', { class: 'amap__p-list' }, ...def.worker.map((t) => h('li', { text: t }))),
			def.by.length
				? h('p', { class: 'amap__by' }, 'Implemented by ', ...def.by.flatMap((b, i) => [i ? ', ' : '', h('code', { text: b })]), def.id === 'ISubscribable' || def.id.startsWith('IDistributed') ? ' (on its NewDistributed queues)' : '')
				: h('p', { class: 'amap__by', text: 'Neither redisq nor sqliteq implements it.' }),
			def.kind !== 'base' && !on ? h('p', { class: 'amap__hint', text: 'Your type does not have it yet. It is off.' }) : null,
		];
		detail.replaceChildren(...kids.filter((k): k is Node => k !== null));

		// Binding calls.
		bindList.replaceChildren(
			...BINDINGS.map((b) => {
				const ok = b.test(caps);
				const missing = b.needs.filter((n) => !defs.get(n)!.on(caps));
				return h(
					'li',
					{ class: `amap__bindrow${ok ? ' is-ok' : ''}` },
					h('code', { text: b.name }),
					h('span', { class: 'amap__verdict', text: ok ? `accepts → ${b.get}` : `needs ${missing.join(' and ')}` }),
				);
			}),
		);

		// Behaviours.
		does.replaceChildren(
			...behaviours(caps).flatMap((b) => [h('dt', { text: b.label }), h('dd', { class: b.on ? 'is-on' : '', text: b.text })]),
		);

		setCall(call, bestCall(caps));
	}

	root.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-node]');
		if (!btn) return;
		const id = btn.dataset.node!;
		const def = defs.get(id)!;
		if (def.kind !== 'base') {
			const next = { ...caps };
			if (def.on(caps) && def.disable) def.disable(next);
			else if (!def.on(caps)) def.enable(next);
			caps = next;
		}
		selected = id;
		render();
	});

	figure.addEventListener('click', (e) => {
		const p = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-preset]');
		if (!p) return;
		caps = { ...(PRESETS.find((x) => x.key === p.dataset.preset)?.caps ?? NO_CAPS) };
		render();
	});

	render();
}
