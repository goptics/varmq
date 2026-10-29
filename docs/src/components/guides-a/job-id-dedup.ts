// Scene: job IDs and the in-queue check. The worker does not start by itself, so the jobs wait in the
// queue. The queue refuses a second given ID while the first job is in the queue. A started worker with
// concurrency 1 takes every waiting job into one batch, so the queue frees all their IDs at once. Jobs
// added while the batch runs wait in the queue and hold their IDs. Purge frees the IDs of the waiting
// jobs and closes the jobs of the batch that have not started.
import { caption, flip, q, say } from './kit';
import { h, reducedMotion, tabs, Timers } from '../ui/dom';
import { IdQueue, type IdJob } from './model';

const RUN_MS = 2600;
const WORKER = 'worker := varmq.NewWorker(fn, varmq.WithAutoRun(false))';
const WORKER_GEN = 'worker := varmq.NewWorker(fn, varmq.WithAutoRun(false), varmq.WithJobIdGenerator(next))';

function init(root: HTMLElement) {
	const timers = new Timers();
	const els = {
		queue: q<HTMLElement>(root, '[data-queue]'),
		run: q<HTMLElement>(root, '[data-run]'),
		batch: q<HTMLElement>(root, '[data-batch]'),
		held: q<HTMLElement>(root, '[data-held]'),
		log: q<HTMLElement>(root, '[data-log]'),
		why: q<HTMLElement>(root, '[data-why]'),
		gen: q<HTMLElement>(root, '[data-gen]'),
	};
	const genBtn = q<HTMLButtonElement>(root, '[data-gen-toggle]');
	const startBtn = q<HTMLButtonElement>(root, '[data-start]');
	const purgeBtn = q<HTMLButtonElement>(root, '[data-purge]');

	let m = new IdQueue();
	let started = false;
	let running: IdJob | null = null;
	/** Jobs the worker took into its batch and has not started. They left the queue, so their IDs are free. */
	let batch: IdJob[] = [];
	/** Batch jobs that Purge reached: the worker closes them, and runs nothing, when it gets to them. */
	const purged = new Set<number>();
	let choice = 'refresh';
	let via: 'Add' | 'AddAll' = 'Add';
	let calls: { text: string; res: string; tone: string }[] = [];
	const chips = new Map<number, HTMLElement>();

	const chipFor = (j: IdJob) => {
		let c = chips.get(j.key);
		if (!c) {
			c = h('span', { class: 'chip chip--id', k: `job-${j.key}`, attrs: { role: 'listitem' } }, h('span', { text: j.label }), h('small', { text: j.id ? `ID ${j.id}` : 'no ID' }));
			chips.set(j.key, c);
		}
		return c;
	};
	const fill = (el: HTMLElement, items: Node[]) => {
		const hint = el.querySelector<HTMLElement>('.ga-hint');
		el.replaceChildren(...(hint ? [hint] : []), ...items);
	};

	const paint = () => {
		fill(els.queue, m.queue.map(chipFor));
		fill(els.run, running ? [chipFor(running)] : []);
		els.run.querySelectorAll('.chip').forEach((c) => c.classList.add('is-run'));
		const runHint = els.run.querySelector<HTMLElement>('.ga-hint');
		if (runHint) runHint.textContent = started ? 'idle' : 'not started';
		fill(els.batch, batch.map(chipFor));
		els.batch.querySelectorAll<HTMLElement>('.chip').forEach((c, i) => c.classList.toggle('is-dim', purged.has(batch[i].key)));
		fill(els.held, [...m.held].map((id) => h('span', { class: 'ga-tag ga-tag--on', text: id, attrs: { role: 'listitem' } })));
		els.gen.textContent = m.generator ? `generator: next is auto-${m.counter + 1}` : 'no generator';
		els.gen.className = `ga-tag${m.generator ? ' ga-tag--on' : ''}`;
		genBtn.setAttribute('aria-pressed', String(m.generator));
		startBtn.disabled = started;
		startBtn.textContent = started ? 'Worker started' : 'Start worker';
		purgeBtn.disabled = !m.queue.length && batch.every((j) => purged.has(j.key));
		els.log.replaceChildren(
			...(calls.length
				? calls.slice(0, 5).map((c) => {
						const li = h('li', {}, h('span', { text: c.text }), h('span', { text: c.res }));
						li.dataset.tone = c.tone;
						return li;
					})
				: [h('li', { class: 'ga-hint', text: 'no calls yet' })]),
		);
	};
	const render = () => flip(root, paint);

	const callText = (label: string) => {
		if (via === 'Add') return choice ? `Add("${label}", WithJobId("${choice}"))` : `Add("${label}")`;
		return `AddAll({${choice ? `ID: "${choice}", ` : ''}Data: "${label}"})`;
	};
	const goCall = (label: string) =>
		via === 'Add'
			? `queue.Add("${label}"${choice ? `, varmq.WithJobId("${choice}")` : ''})`
			: `queue.AddAll([]varmq.Item[string]{{${choice ? `ID: "${choice}", ` : ''}Data: "${label}"}})`;

	const log = (label: string, res: string, tone: string, why: string) => {
		calls.unshift({ text: callText(label), res, tone });
		caption(root, goCall(label));
		els.why.textContent = why;
	};

	// ---- the worker: concurrency 1, so one goroutine and one batch at a time ----

	/** A started worker whose goroutine is free takes every waiting job, up to 32, into one batch. */
	const pull = (): void => {
		if (!started || running || batch.length) return;
		const taken = m.takeBatch();
		if (!taken.length) return;
		batch = taken;
		const names = taken.map((j) => j.label).join(', ');
		const ids = taken.filter((j) => j.given).map((j) => `"${j.id}"`);
		calls.unshift({ text: `worker takes ${taken.length} ${taken.length === 1 ? 'job' : 'jobs'}`, res: ids.length ? `frees ${ids.join(', ')}` : 'frees nothing', tone: '' });
		els.why.textContent = ids.length
			? `The worker took ${names} into one batch, and the queue freed ${ids.join(' and ')}. The same ${ids.length === 1 ? 'ID is' : 'IDs are'} accepted again now, even while ${taken.length === 1 ? 'the job runs' : 'the jobs run'}.`
			: `The worker took ${names} into one batch. No job had a given ID, so the queue held nothing for them.`;
		say(root, `The worker took ${names}.${ids.length ? ` ${ids.join(' and ')} ${ids.length === 1 ? 'is' : 'are'} free.` : ''}`);
		next();
	};

	/** The goroutine runs its batch in order and closes a purged job instead of running it. When the batch ends, it takes what waits. */
	const next = (): void => {
		while (batch.length && purged.has(batch[0].key)) purged.delete(batch.shift()!.key);
		running = batch.shift() ?? null;
		if (!running) return pull();
		if (reducedMotion()) {
			running = null;
			return next();
		}
		timers.after(RUN_MS, () => {
			running = null;
			next();
			render();
		});
	};

	q(root, '[data-add]').addEventListener('click', () => {
		const label = m.nextLabel;
		const r = m.add(via, choice);
		if (!r.ok) {
			if (via === 'AddAll') {
				log(label, 'NumPending() = 0', 'bad', `Skipped: a job with the ID "${r.id}" is still in the queue. AddAll still returns the group, and its NumPending() is 0. It does not say which item it skipped.`);
				say(root, `AddAll skipped the item. The ID ${r.id} is already in the queue.`);
			} else {
				log(label, 'nil, false', 'bad', `Refused: a job with the ID "${r.id}" is still in the queue. The queue stores one job for each given ID. Add only returns false: it does not say why.`);
				say(root, `Add returned nil and false. The ID ${r.id} is already in the queue.`);
			}
			render();
			return;
		}
		const gen = r.discarded ? ` The generator ran and gave ${r.discarded}. WithJobId took its place, and the value was thrown away.` : '';
		if (via === 'AddAll') {
			log(label, `NumPending() = 1`, '', `Queued with the ID "${r.id}". AddAll adds the prefix g:, so it never collides with an Add of the same ID.`);
		} else if (!r.job!.given) {
			log(label, `ID() = "${r.id}"`, '', r.id ? `A generated ID is not checked, so a second job with no ID of its own is stored too.${gen}` : 'A job with no ID is never checked. A second one is stored too.');
		} else {
			log(label, `ID() = "${r.id}"`, '', `Stored. Add the same ID again to see the queue refuse it.${gen}`);
		}
		say(root, `${via} stored ${label}${r.id ? ` with the ID ${r.id}` : ' with no ID'}.`);
		pull(); // a started worker with a free goroutine takes the job at once
		render();
	});

	startBtn.addEventListener('click', () => {
		started = true;
		caption(root, 'worker.Start() // takes every waiting job, up to 32, into one batch');
		els.why.textContent = 'The worker started. The queue is empty, so it waits. A job added now leaves the queue at once.';
		pull();
		render();
	});

	purgeBtn.addEventListener('click', () => {
		const gone = m.purge();
		const inBatch = batch.filter((j) => !purged.has(j.key));
		for (const j of inBatch) purged.add(j.key);
		const n = gone.length + inBatch.length;
		calls.unshift({ text: 'queue.Purge()', res: `closes ${n}`, tone: 'warn' });
		caption(root, 'queue.Purge()');
		els.why.textContent = inBatch.length
			? 'Purge removed every waiting job and freed its ID. It also closes the jobs of the batch that have not started, so the worker skips them. The running job finishes.'
			: 'Purge removed every waiting job and freed every ID.';
		render();
		say(root, `Purge closed ${n} jobs.`);
	});

	genBtn.addEventListener('click', () => {
		m.generator = !m.generator;
		caption(root, m.generator ? WORKER_GEN : WORKER);
		els.why.textContent = m.generator
			? 'Add calls the generator every time, also when you pass WithJobId, and throws the value away. AddAll calls it only for an item without an ID. A generated ID is never checked.'
			: 'Without a generator, a job with no ID of its own has none.';
		render();
		say(root, m.generator ? 'Generator on.' : 'Generator off.');
	});

	q(root, '[data-reset]').addEventListener('click', () => {
		timers.clear();
		const gen = m.generator;
		m = new IdQueue();
		m.generator = gen;
		started = false;
		running = null;
		batch = [];
		purged.clear();
		calls = [];
		chips.clear();
		els.why.textContent = 'Add a job with the ID refresh, then add it again.';
		caption(root, 'queue.Add("job 1", varmq.WithJobId("refresh"))');
		render();
		say(root, 'Reset.');
	});

	tabs(q(root, '[data-ids]'), (k) => {
		choice = k;
	});
	const addBtn = q<HTMLButtonElement>(root, '[data-add]');
	tabs(q(root, '[data-via]'), (k) => {
		via = k as 'Add' | 'AddAll';
		addBtn.textContent = via === 'Add' ? 'Add job' : 'AddAll one item';
	});

	els.why.textContent = 'Add a job with the ID refresh, then add it again.';
	paint();
	root.dataset.ready = '';
}

document.querySelectorAll<HTMLElement>('[data-scene="job-id-dedup"]').forEach(init);
