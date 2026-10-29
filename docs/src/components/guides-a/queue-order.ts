// Scene: a first-in first-out queue next to a priority queue. The same jobs go into both, a worker
// with concurrency 1 takes them one at a time, and each queue shows the order they leave in.
import { caption, chip, flip, q, say, stepper } from './kit';
import { reducedMotion, Timers } from '../ui/dom';
import { nextRanked } from './model';

const NAMES = ['report', 'alert', 'digest', 'invoice', 'backup', 'export', 'sync', 'email'];
const RUN_MS = 900;
const FIRST_CAP = 'queue.Add("report")   pq.Add("report", 10)';

interface Job {
	name: string;
	priority: number;
	seq: number;
}

interface Side {
	kind: 'fifo' | 'pq';
	waiting: HTMLElement;
	run: HTMLElement;
	done: HTMLElement;
	len: HTMLElement;
	queue: Job[];
	running: Job | null;
	ran: Job[];
}

function init(root: HTMLElement) {
	const timers = new Timers();
	const side = (kind: 'fifo' | 'pq'): Side => {
		const el = q<HTMLElement>(root, `[data-side="${kind}"]`);
		return {
			kind,
			waiting: q(el, '[data-waiting]'),
			run: q(el, '[data-run]'),
			done: q(el, '[data-done]'),
			len: q(el, '[data-len]'),
			queue: [],
			running: null,
			ran: [],
		};
	};
	const sides = [side('fifo'), side('pq')];
	const chips = new Map<string, HTMLElement>();
	let seq = 0;
	let names = 0;
	let started = false;
	let paused = false;
	let pending = false;

	const startBtn = q<HTMLButtonElement>(root, '[data-start]');
	const prio = stepper(q(root, '[data-prio]'), { min: -9, max: 20, value: 10, onChange: () => {} });

	const key = (s: Side, j: Job) => `${s.kind}-${j.seq}`;
	const chipFor = (s: Side, j: Job) => {
		let c = chips.get(key(s, j));
		if (!c) {
			const label = s.kind === 'pq' ? `${j.name} ${j.priority}` : j.name;
			c = chip(label, s.kind === 'pq' && j.priority < 5 ? 'amber' : '', key(s, j));
			chips.set(key(s, j), c);
		}
		return c;
	};

	// The rail shows the order the jobs leave in.
	const order = (s: Side): Job[] => {
		if (s.kind === 'fifo') return s.queue;
		const rest = [...s.queue];
		const out: Job[] = [];
		for (let j = nextRanked(rest); j; j = nextRanked(rest)) {
			out.push(j);
			rest.splice(rest.indexOf(j), 1);
		}
		return out;
	};

	const fill = (el: HTMLElement, items: HTMLElement[], hint?: string) => {
		const h = el.querySelector<HTMLElement>('.ga-hint');
		if (h && hint) h.textContent = hint;
		el.replaceChildren(...(h ? [h] : []), ...items);
	};

	const paint = () => {
		for (const s of sides) {
			fill(s.waiting, order(s).map((j) => chipFor(s, j)));
			fill(s.run, s.running ? [chipFor(s, s.running)] : [], started ? 'idle' : 'not started');
			s.run.querySelectorAll('.chip').forEach((c) => c.classList.add('is-run'));
			fill(s.done, s.ran.map((j) => chipFor(s, j)));
			s.done.querySelectorAll<HTMLElement>('.chip').forEach((c, i) => {
				c.classList.remove('is-run');
				c.dataset.n = String(i + 1);
			});
			s.len.textContent = `${s.queue.length} ${s.queue.length === 1 ? 'job' : 'jobs'}`;
		}
		startBtn.textContent = reducedMotion() ? 'Run all' : !started ? 'Start workers' : paused ? 'Resume workers' : 'Pause workers';
		startBtn.disabled = started && !paused && !reducedMotion() && !busy() && !sides.some((s) => s.queue.length); // nothing to pause
	};
	const render = () => flip(root, paint);

	const take = (s: Side) => {
		const next = order(s)[0];
		if (!next || s.running) return;
		s.queue.splice(s.queue.indexOf(next), 1);
		s.running = next;
	};
	const finish = (s: Side) => {
		if (s.running) s.ran.push(s.running);
		s.running = null;
	};
	const busy = () => sides.some((s) => s.running);

	const schedule = () => {
		if (pending || !busy()) return;
		pending = true;
		timers.after(RUN_MS, () => {
			pending = false;
			tick();
		});
	};

	const tick = () => {
		const done = sides[1].running;
		for (const s of sides) finish(s);
		if (!paused) for (const s of sides) take(s);
		render();
		if (done) say(root, `Ran ${done.name}.`);
		if (!busy() && !paused) say(root, 'Both queues are empty.');
		schedule();
	};

	/** With reduced motion the worker runs everything at once and the scene shows the end state. */
	const drain = () => {
		do {
			for (const s of sides) take(s);
			for (const s of sides) finish(s);
		} while (sides.some((s) => s.queue.length));
		render();
	};

	const run = () => {
		for (const s of sides) take(s);
		render();
		schedule();
	};

	const order_of = (s: Side) => order(s).map((x) => x.name).join(', ');

	const add = (name: string, priority: number) => {
		seq++;
		for (const s of sides) s.queue.push({ name, priority, seq });
		caption(root, `queue.Add("${name}")   pq.Add("${name}", ${priority})`);
		if (started && reducedMotion()) drain();
		else if (started && !paused) {
			for (const s of sides) take(s);
			render();
			schedule();
		} else render();
		say(root, `Added ${name} with priority ${priority}. First queue order: ${order_of(sides[0])}. Priority queue order: ${order_of(sides[1])}.`);
	};

	const reset = () => {
		timers.clear();
		pending = false;
		started = false;
		paused = false;
		seq = 0;
		names = 0;
		for (const s of sides) {
			s.queue = [];
			s.running = null;
			s.ran = [];
		}
		chips.clear();
		paint();
		caption(root, FIRST_CAP);
		say(root, 'Reset. Both queues are empty.');
	};

	q(root, '[data-add]').addEventListener('click', () => add(NAMES[names++ % NAMES.length], prio.get()));
	q(root, '[data-example]').addEventListener('click', () => {
		reset();
		for (const [name, p] of [['report', 10], ['alert', 1], ['digest', 10]] as const) add(name, p);
		names = 3;
	});
	startBtn.addEventListener('click', () => {
		if (reducedMotion()) {
			started = true;
			drain();
			say(root, 'Everything ran. The first queue ran the jobs in add order. The priority queue ran the lowest number first.');
		} else if (!started || paused) {
			const resuming = started;
			started = true;
			paused = false;
			caption(root, resuming ? 'worker.Resume()' : 'worker.Start()');
			run();
		} else {
			paused = true;
			caption(root, 'worker.Pause()');
			paint();
			say(root, 'Both workers paused. Jobs that run now finish. No new job starts.');
		}
	});
	q(root, '[data-reset]').addEventListener('click', reset);

	root.dataset.ready = '';
	paint();
}

document.querySelectorAll<HTMLElement>('[data-scene="queue-order"]').forEach(init);
