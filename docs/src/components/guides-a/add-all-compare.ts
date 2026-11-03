// Scene: the same items added with Add, one call each, and with AddAll, one call for the group. The
// queue refuses an item that repeats an ID in the queue, or does not fit. Add tells you which item.
// AddAll skips it and does not say which. The worker has concurrency 3, so results arrive in the
// order the jobs finish, not in the order of the slice.
import { caption, flip, q, say } from './kit';
import { h, reducedMotion, tabs, Timers } from '../ui/dom';

const CONCURRENCY = 3;
const DURATIONS = [700, 1100, 1500, 1900];

interface Item {
	id: string;
	data: string;
}

type Variant = 'new' | 'repeat' | 'full';

const ITEMS: Record<Variant, Item[]> = {
	new: [
		{ id: 'a', data: '10' },
		{ id: 'b', data: 'x' },
		{ id: 'c', data: '30' },
		{ id: 'd', data: '40' },
	],
	repeat: [
		{ id: 'a', data: '10' },
		{ id: 'b', data: 'x' },
		{ id: 'a', data: '20' },
		{ id: 'c', data: '30' },
	],
	full: [
		{ id: 'a', data: '10' },
		{ id: 'b', data: 'x' },
		{ id: 'c', data: '30' },
		{ id: 'd', data: '40' },
	],
};

const parse = (d: string): { n?: number; err?: string } => (/^\d+$/.test(d) ? { n: Number(d) } : { err: `strconv.Atoi: parsing "${d}": invalid syntax` });

function init(root: HTMLElement) {
	const timers = new Timers();
	const els = {
		items: q<HTMLElement>(root, '[data-items]'),
		note: q<HTMLElement>(root, '[data-items-note]'),
		rows: q<HTMLElement>(root, '[data-rows]'),
		addNote: q<HTMLElement>(root, '[data-add-note]'),
		pending: q<HTMLElement>(root, '[data-pending]'),
		wait: q<HTMLElement>(root, '[data-wait]'),
		results: q<HTMLElement>(root, '[data-results]'),
		allNote: q<HTMLElement>(root, '[data-all-note]'),
	};
	const addBtn = q<HTMLButtonElement>(root, '[data-add]');
	const startBtn = q<HTMLButtonElement>(root, '[data-start]');

	let variant: Variant = 'repeat';
	let phase: 'idle' | 'added' | 'running' | 'done' = 'idle';
	let accepted: boolean[] = [];
	let status: string[] = []; // per item: '', 'Queued', 'Processing', 'Finished'
	let arrived: number[] = []; // item indexes, in the order the jobs finished
	let pending = 0;
	let startPending = 0;

	const items = () => ITEMS[variant];
	const capacity = () => (variant === 'full' ? 3 : 0);

	const acceptAll = (prefix: string): boolean[] => {
		const held = new Set<string>();
		let count = 0;
		return items().map((it) => {
			const id = prefix + it.id;
			if ((capacity() > 0 && count >= capacity()) || held.has(id)) return false;
			held.add(id);
			count++;
			return true;
		});
	};

	const itemChip = (it: Item, i: number) =>
		h('span', { class: 'chip chip--id', k: `item-${i}`, attrs: { role: 'listitem' } }, h('span', { text: `ID ${it.id}` }), h('small', { text: `Data "${it.data}"` }));

	const paint = () => {
		els.items.replaceChildren(...items().map(itemChip));
		els.note.textContent = variant === 'repeat' ? 'the third item repeats the ID a' : variant === 'full' ? 'the queue holds 3 jobs, and 4 items come' : 'four items, four IDs';
		const added = phase !== 'idle';

		// Add, one call for each item: every call says what happened to its item.
		els.rows.replaceChildren(
			...items().map((it, i) => {
				const call = `Add("${it.data}", WithJobId("${it.id}"))`;
				const res = !added ? 'not called' : accepted[i] ? 'handle, true' : 'nil, false';
				const out = !added || !accepted[i] ? '' : status[i] === 'Finished' ? (parse(it.data).err ? `Result() = 0, Err` : `Result() = ${parse(it.data).n}, nil`) : status[i] || '';
				const li = h('li', {}, h('span', { text: call }), h('span', { class: 'ga-tag', text: res }), h('span', { text: out }));
				li.dataset.tone = added ? (accepted[i] ? '' : 'bad') : 'dim';
				return li;
			}),
		);
		const refused = accepted.filter((a) => !a).length;
		els.addNote.textContent = added
			? refused
				? `Item ${accepted.indexOf(false) + 1} came back as nil, false. You know which one.`
				: 'Every call returned a handle and true.'
			: '';

		// AddAll, one call for the group.
		const total = items().length;
		els.pending.textContent = !added ? 'not called' : `${pending} of ${total}`;
		els.pending.dataset.tone = !added ? 'dim' : startPending < total ? 'warn' : '';
		els.wait.textContent = !added ? 'not called' : phase === 'done' ? 'returned: NumPending() is 0' : phase === 'running' ? 'blocked until NumPending() is 0' : 'blocked';
		els.wait.dataset.tone = phase === 'done' ? 'ok' : added ? '' : 'dim';
		const hint = els.results.querySelector<HTMLElement>('.ga-hint');
		els.results.replaceChildren(
			...(hint ? [hint] : []),
			...arrived.map((i, n) => {
				const it = items()[i];
				const r = parse(it.data);
				const c = h('span', { class: `chip${r.err ? ' chip--danger' : ''}`, text: `g:${it.id} ${r.err ? 'Err' : r.n}`, k: `res-${i}`, attrs: { role: 'listitem' } });
				c.dataset.n = String(n + 1);
				return c;
			}),
		);
		els.allNote.textContent = !added
			? ''
			: refused
				? `NumPending() started at ${startPending}, below ${total}. AddAll skipped ${refused === 1 ? 'one item' : `${refused} items`} and does not say which. A skipped item never sends a result.`
				: 'No item was skipped.';
		if (phase === 'done') els.allNote.textContent += ' The channel closes, so a range loop ends by itself.';

		addBtn.disabled = phase !== 'idle';
		startBtn.disabled = phase !== 'added';
	};
	const render = () => flip(root, paint);

	const reset = () => {
		timers.clear();
		phase = 'idle';
		accepted = [];
		status = [];
		arrived = [];
		pending = 0;
		startPending = 0;
		caption(root, 'group := queue.AddAll(items)');
		paint();
	};

	addBtn.addEventListener('click', () => {
		accepted = acceptAll('');
		status = accepted.map((a) => (a ? 'Queued' : ''));
		pending = accepted.filter(Boolean).length;
		startPending = pending;
		phase = 'added';
		caption(root, 'group := queue.AddAll(items)');
		render();
		say(root, `Add accepted ${pending} of ${items().length} items and told you which. AddAll accepted the same ${pending}, and NumPending is ${pending}. It does not say which item it skipped.`);
	});

	startBtn.addEventListener('click', () => {
		// The timeline of a worker with concurrency 3: the first three jobs start together, and each finish starts the next.
		const jobs = accepted.map((a, i) => (a ? i : -1)).filter((i) => i >= 0);
		const order = [...DURATIONS].sort(() => Math.random() - 0.5);
		const dur = new Map(jobs.map((i, n) => [i, order[n % order.length]]));
		const free: number[] = Array(CONCURRENCY).fill(0);
		const plan = jobs.map((i) => {
			const lane = free.indexOf(Math.min(...free));
			const start = free[lane];
			free[lane] = start + dur.get(i)!;
			return { i, start, end: free[lane] };
		});
		phase = 'running';
		caption(root, 'worker.Start()');
		if (reducedMotion()) {
			for (const p of [...plan].sort((a, b) => a.end - b.end)) {
				status[p.i] = 'Finished';
				arrived.push(p.i);
				pending--;
			}
			phase = 'done';
			render();
			say(root, 'Every job finished. NumPending is 0 and Wait returned.');
			return;
		}
		for (const p of plan) {
			timers.after(p.start, () => {
				status[p.i] = 'Processing';
				render();
			});
			timers.after(p.end, () => {
				status[p.i] = 'Finished';
				arrived.push(p.i);
				pending--;
				if (pending === 0) phase = 'done';
				render();
				say(root, `Job ${items()[p.i].id} finished. NumPending is ${pending}.`);
			});
		}
		render();
	});

	q(root, '[data-reset]').addEventListener('click', reset);
	tabs(q(root, '[data-variants]'), (k) => {
		variant = k as Variant;
		reset();
	});

	reset();
	root.dataset.ready = '';
}

document.querySelectorAll<HTMLElement>('[data-scene="add-all-compare"]').forEach(init);
