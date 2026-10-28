// Scene: a bounded queue and its handle. Add refuses a job when the queue is full or closed. Len and
// IsFull read the queue. A started worker takes the waiting jobs into a batch, and the batch is
// outside the queue. Purge closes every job that has not started. Close stops the queue taking jobs.
import { caption, chip, flip, q, say, stepper } from './kit';
import { h, reducedMotion, Timers } from '../ui/dom';

const RUN_MS = 900;
const CLOSE_MS = 350;
const FIRST_CAP = 'queue := worker.BindQueue(varmq.WithQueueCapacity(2))';

interface Job {
	n: number;
	/** Purge reached this job after the worker took it into a batch: the worker closes it as it gets there. */
	purged: boolean;
}

function init(root: HTMLElement) {
	const timers = new Timers();
	const rail = (s: string) => q<HTMLElement>(root, s);
	const els = {
		queue: rail('[data-queue]'),
		run: rail('[data-run]'),
		batch: rail('[data-batch]'),
		closed: rail('[data-closed]'),
		ran: rail('[data-ran]'),
		len: rail('[data-len]'),
		full: rail('[data-full]'),
		add: rail('[data-add-result]'),
		why: rail('[data-why]'),
		state: rail('[data-state]'),
	};
	const startBtn = q<HTMLButtonElement>(root, '[data-start]');
	const closeBtn = q<HTMLButtonElement>(root, '[data-close]');

	let capacity = 2;
	let queue: Job[] = [];
	let batch: Job[] = [];
	let running: Job | null = null;
	let closedJobs: Job[] = [];
	let ran: Job[] = [];
	let isClosed = false;
	let started = false;
	let n = 0;
	let pending = false;
	const chips = new Map<number, HTMLElement>();

	const cap = stepper(q(root, '[data-cap-step]'), {
		min: 0,
		max: 5,
		value: capacity,
		format: (v) => (v < 1 ? 'no limit' : String(v)),
		onChange: (v) => {
			capacity = v;
			caption(root, `queue := worker.BindQueue(varmq.WithQueueCapacity(${v}))`);
			render();
		},
	});

	const chipFor = (j: Job) => {
		let c = chips.get(j.n);
		if (!c) {
			c = chip(`job ${j.n}`, '', j.n);
			chips.set(j.n, c);
		}
		return c;
	};
	const fill = (el: HTMLElement, items: Node[]) => {
		const hint = el.querySelector<HTMLElement>('.ga-hint');
		el.replaceChildren(...(hint ? [hint] : []), ...items);
	};
	const isFull = () => capacity > 0 && queue.length >= capacity;

	const paint = () => {
		// The queue shows an empty slot for each place that is still free.
		const slots = capacity > 0 ? Array.from({ length: Math.max(0, capacity - queue.length) }, () => h('span', { class: 'chip chip--free', attrs: { 'aria-hidden': 'true' }, text: 'free' })) : [];
		els.queue.replaceChildren(...queue.map(chipFor), ...slots);
		if (capacity < 1 && !queue.length) els.queue.append(h('span', { class: 'ga-hint', text: 'no limit, empty' }));
		fill(els.run, running ? [chipFor(running)] : []);
		els.run.querySelectorAll('.chip').forEach((c) => c.classList.add('is-run'));
		const hint = els.run.querySelector<HTMLElement>('.ga-hint');
		if (hint) hint.textContent = started ? 'idle' : 'not started';
		fill(els.batch, batch.map(chipFor));
		fill(els.closed, closedJobs.map(chipFor));
		els.closed.querySelectorAll('.chip').forEach((c) => c.classList.add('is-dim'));
		fill(els.ran, ran.map(chipFor));
		els.ran.querySelectorAll('.chip').forEach((c) => c.classList.remove('is-run'));
		els.len.textContent = String(queue.length);
		els.full.textContent = String(isFull());
		els.full.dataset.tone = isFull() ? 'warn' : '';
		els.state.textContent = isClosed ? 'closed' : capacity > 0 ? `capacity ${capacity}` : 'no limit';
		els.state.className = `ga-tag${isClosed ? ' ga-tag--bad' : ''}`;
		startBtn.textContent = reducedMotion() ? 'Run all' : started ? 'Worker started' : 'Start worker';
		startBtn.disabled = started && !reducedMotion();
		closeBtn.disabled = isClosed;
	};
	const render = () => flip(root, paint);

	const result = (text: string, tone: string, why = '') => {
		els.add.textContent = text;
		els.add.dataset.tone = tone;
		els.why.textContent = why;
	};
	const explain = (why: string) => {
		els.why.textContent = why;
	};

	// ---- the worker ----
	const pull = () => {
		// A worker that is free takes the waiting jobs into a batch: they leave the queue (at most 32).
		if (!started || running || batch.length || !queue.length) return;
		batch = queue.splice(0, 32);
	};
	const next = () => {
		if (running || !batch.length) return;
		const j = batch.shift()!;
		if (j.purged) {
			closedJobs.push(j); // the worker closes a purged job as it reaches it, and runs nothing
			return next();
		}
		running = j;
	};
	const advance = () => {
		pull();
		next();
	};
	const schedule = () => {
		if (pending || !running) return;
		pending = true;
		timers.after(RUN_MS, () => {
			pending = false;
			if (running) ran.push(running);
			running = null;
			advance();
			render();
			schedule();
			if (!running && !queue.length && !batch.length) say(root, 'The worker is idle.');
		});
	};
	const drain = () => {
		for (;;) {
			pull();
			next();
			if (!running) break;
			ran.push(running);
			running = null;
		}
	};

	q(root, '[data-add]').addEventListener('click', () => {
		const label = `job ${n + 1}`;
		if (isClosed || isFull()) {
			caption(root, `queue.Add("${label}")`);
			result('nil, false', 'bad', isClosed ? 'The queue is closed. Add only returns false: it does not say why.' : 'The queue is full. Add does not block. It returns false.');
			say(root, `Add returned nil and false. ${isClosed ? 'The queue is closed.' : 'The queue is full.'}`);
			paint();
			return;
		}
		const j: Job = { n: ++n, purged: false };
		queue.push(j);
		caption(root, `queue.Add("${label}")`);
		result(`job ${j.n}, true`, 'ok', `There is room: ${capacity > 0 ? `${queue.length} of ${capacity} places are taken.` : 'the queue has no limit.'}`);
		if (started && reducedMotion()) drain();
		else if (started) advance();
		render();
		schedule();
		say(root, `Add returned job ${j.n} and true.`);
	});

	q(root, '[data-purge]').addEventListener('click', () => {
		const gone = queue.splice(0);
		for (const j of gone) closedJobs.push(j);
		for (const j of batch) j.purged = true;
		caption(root, 'queue.Purge()');
		explain(gone.length || batch.length ? 'Purge closed every job that has not started. A running job is not affected.' : 'The queue was empty.');
		render();
		say(root, `Purge closed ${gone.length} waiting ${gone.length === 1 ? 'job' : 'jobs'}.`);
	});

	closeBtn.addEventListener('click', () => {
		isClosed = true;
		caption(root, 'queue.Close()');
		explain('Every later Add returns false. The jobs in the queue stay and still run.');
		render();
		say(root, 'The queue is closed. Jobs inside still run.');
	});

	startBtn.addEventListener('click', () => {
		started = true;
		caption(root, 'worker.Start()');
		if (reducedMotion()) {
			drain();
			render();
			say(root, 'The worker ran every job.');
			return;
		}
		advance();
		render();
		schedule();
		say(root, 'Worker started. It took the waiting jobs into a batch, so the queue has room again.');
	});

	q(root, '[data-reset]').addEventListener('click', () => {
		timers.clear();
		pending = false;
		queue = [];
		batch = [];
		running = null;
		closedJobs = [];
		ran = [];
		isClosed = false;
		started = false;
		n = 0;
		chips.clear();
		capacity = 2;
		cap.set(2);
		result('not called', 'dim');
		caption(root, FIRST_CAP);
		say(root, 'Reset.');
	});

	root.dataset.ready = '';
	paint();
}

document.querySelectorAll<HTMLElement>('[data-scene="queue-handle"]').forEach(init);
