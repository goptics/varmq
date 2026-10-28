// Scene: job IDs and the in-queue check. The worker does not start by itself, so the jobs wait in the
// queue. The queue refuses a second given ID while the first job is in the queue, and frees the ID
// when a worker takes the job or when Purge removes it.
import { caption, flip, h, q, reduced, say, tabs, Timers } from './kit';
import { IdQueue, type IdJob } from './model';

const RUN_MS = 2600;

function init(root: HTMLElement) {
	const timers = new Timers();
	const els = {
		queue: q<HTMLElement>(root, '[data-queue]'),
		run: q<HTMLElement>(root, '[data-run]'),
		held: q<HTMLElement>(root, '[data-held]'),
		log: q<HTMLElement>(root, '[data-log]'),
		why: q<HTMLElement>(root, '[data-why]'),
		gen: q<HTMLElement>(root, '[data-gen]'),
	};
	const genBtn = q<HTMLButtonElement>(root, '[data-gen-toggle]');
	const takeBtn = q<HTMLButtonElement>(root, '[data-take]');
	const purgeBtn = q<HTMLButtonElement>(root, '[data-purge]');

	let m = new IdQueue();
	let running: IdJob | null = null;
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
		fill(els.held, [...m.held].map((id) => h('span', { class: 'ga-tag ga-tag--on', text: id, attrs: { role: 'listitem' } })));
		els.gen.textContent = m.generator ? `generator: next is auto-${m.counter + 1}` : 'no generator';
		els.gen.className = `ga-tag${m.generator ? ' ga-tag--on' : ''}`;
		genBtn.setAttribute('aria-pressed', String(m.generator));
		takeBtn.disabled = !!running || !m.queue.length;
		purgeBtn.disabled = !m.queue.length;
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
		render();
	};

	q(root, '[data-add]').addEventListener('click', () => {
		const label = m.nextLabel;
		const r = m.add(via, choice);
		if (!r.ok) {
			log(label, 'nil, false', 'bad', `Refused: a job with the ID "${r.id}" is still in the queue. The queue stores one job for each given ID. Add only returns false: it does not say why.`);
			say(root, `${via} returned nil and false. The ID ${r.id} is already in the queue.`);
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
	});

	takeBtn.addEventListener('click', () => {
		const j = m.take();
		if (!j) return;
		running = j;
		caption(root, 'worker takes a job from the queue');
		els.why.textContent = j.given
			? `The worker took ${j.label}, and the queue freed the ID "${j.id}". The same ID is accepted again now, even while ${j.label} runs.`
			: `The worker took ${j.label}. It had no given ID, so the queue held nothing for it.`;
		calls.unshift({ text: 'worker takes a job', res: j.given ? `frees "${j.id}"` : 'frees nothing', tone: '' });
		render();
		say(root, `The worker took ${j.label}.${j.given ? ` The ID ${j.id} is free.` : ''}`);
		const done = () => {
			running = null;
			render();
		};
		if (reduced()) done();
		else timers.after(RUN_MS, done);
	});

	purgeBtn.addEventListener('click', () => {
		const gone = m.purge();
		calls.unshift({ text: 'queue.Purge()', res: `closed ${gone.length}`, tone: 'warn' });
		caption(root, 'queue.Purge()');
		els.why.textContent = 'Purge removed every waiting job and freed every ID.';
		render();
		say(root, `Purge closed ${gone.length} jobs and freed their IDs.`);
	});

	genBtn.addEventListener('click', () => {
		m.generator = !m.generator;
		caption(root, m.generator ? 'worker := varmq.NewWorker(fn, varmq.WithJobIdGenerator(next))' : 'worker := varmq.NewWorker(fn)');
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
		running = null;
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

document.querySelectorAll<HTMLElement>('[data-ga="job-id-dedup"]').forEach(init);
