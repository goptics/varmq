// Scene: one worker, three queues, four strategies. The worker takes one job at a time, as varmq does
// when two or more queues are bound, and picks the queue again for every job.
import { caption, chip, flip, h, q, reduced, say, tabs, Timers } from './kit';
import { pick, type StrategyName } from './model';

const STEP_MS = 1200;

interface Queue {
	name: string;
	short: string;
	rank: number | null;
	tone: '' | 'amber' | 'cobalt';
	jobs: number[];
	count: number;
}

const START = [
	{ name: 'alerts', short: 'alert', rank: 1, tone: 'amber' as const, jobs: 2 },
	{ name: 'reports', short: 'report', rank: 2, tone: 'cobalt' as const, jobs: 1 },
	{ name: 'emails', short: 'email', rank: null, tone: '' as const, jobs: 3 },
];

const CAPTIONS: Record<StrategyName, string> = {
	Priority: 'worker := varmq.NewWorker(process, 1)   // Priority, the default',
	RoundRobin: 'worker := varmq.NewWorker(process, 1, varmq.WithStrategy(varmq.RoundRobin))',
	MaxLen: 'worker := varmq.NewWorker(process, 1, varmq.WithStrategy(varmq.MaxLen))',
	MinLen: 'worker := varmq.NewWorker(process, 1, varmq.WithStrategy(varmq.MinLen))',
};

function init(root: HTMLElement) {
	const timers = new Timers();
	const queuesEl = q<HTMLElement>(root, '[data-queues]');
	const orderEl = q<HTMLElement>(root, '[data-order]');
	const whyEl = q<HTMLElement>(root, '[data-why]');
	const countEl = q<HTMLElement>(root, '[data-count]');
	const stepBtn = q<HTMLButtonElement>(root, '[data-step]');
	const playBtn = q<HTMLButtonElement>(root, '[data-play]');
	const panel = q<HTMLElement>(root, '[data-panel]');

	let strategy: StrategyName = 'Priority';
	let queues: Queue[] = [];
	let taken: { key: string; label: string; tone: Queue['tone'] }[] = [];
	let rr = { i: 0 };
	let playing = false;
	let total = 0;
	const chips = new Map<string, HTMLElement>();
	const rails: HTMLElement[] = [];
	const tags: HTMLElement[] = [];

	const build = () => {
		queuesEl.replaceChildren();
		rails.length = 0;
		tags.length = 0;
		for (const [i, qu] of queues.entries()) {
			const turn = h('span', { class: 'ga-tag ga-tag--on', text: 'turn' });
			const rank = qu.rank === null ? 'no priority: ranks last' : `WithQueuePriority(${qu.rank})`;
			const len = h('span', { class: 'ga-tag', text: '0 jobs' });
			const label = h('p', { class: 'ga-label' }, h('strong', { text: qu.name }), h('span', { text: `${rank} · list position ${i + 1}` }), turn, len);
			const rail = h('div', { class: 'ga-rail', attrs: { role: 'list', 'aria-label': `${qu.name} queue` } }, h('span', { class: 'ga-hint', text: 'empty', attrs: { 'aria-hidden': 'true' } }));
			queuesEl.append(h('div', { class: 'ga-queue' }, label, rail));
			rails.push(rail);
			tags.push(turn, len);
		}
	};

	const reset = (s: StrategyName = strategy) => {
		timers.clear();
		playing = false;
		strategy = s;
		queues = START.map((d) => ({ name: d.name, short: d.short, rank: d.rank, tone: d.tone, jobs: [], count: 0 }));
		for (const [i, d] of START.entries()) for (let n = 0; n < d.jobs; n++) queues[i].jobs.push(++queues[i].count);
		taken = [];
		rr = { i: 0 };
		total = START.reduce((n, d) => n + d.jobs, 0);
		chips.clear();
		caption(root, CAPTIONS[s]);
		build();
		paint();
	};

	const key = (qu: Queue, n: number) => `${qu.short}-${n}`;
	const chipFor = (qu: Queue, n: number) => {
		const k = key(qu, n);
		let c = chips.get(k);
		if (!c) {
			c = chip(`${qu.short} ${n}`, qu.tone, k);
			chips.set(k, c);
		}
		return c;
	};

	const preview = () => pick(strategy, queues.map((x) => ({ name: x.name, len: x.jobs.length })), { ...rr });

	const paint = () => {
		const next = preview();
		for (const [i, qu] of queues.entries()) {
			const rail = rails[i];
			const hint = rail.querySelector<HTMLElement>('.ga-hint');
			rail.replaceChildren(...(hint ? [hint] : []), ...qu.jobs.map((n) => chipFor(qu, n)));
			rail.querySelectorAll('.chip').forEach((c, j) => c.classList.toggle('is-next', next?.index === i && j === 0));
			const [turn, len] = [tags[i * 2], tags[i * 2 + 1]];
			len.textContent = `${qu.jobs.length} ${qu.jobs.length === 1 ? 'job' : 'jobs'}`;
			turn.hidden = !(strategy === 'RoundRobin' && rr.i === i);
		}
		const hintO = orderEl.querySelector<HTMLElement>('.ga-hint');
		orderEl.replaceChildren(...(hintO ? [hintO] : []), ...taken.map((t) => chips.get(t.key)!));
		orderEl.querySelectorAll<HTMLElement>('.chip').forEach((c, i) => {
			c.dataset.n = String(i + 1);
			c.classList.remove('is-next');
		});
		countEl.textContent = `${taken.length} of ${total} jobs`;
		if (next) {
			whyEl.textContent = `Next: ${queues[next.index].short} from ${queues[next.index].name}. ${next.why}`;
		} else {
			whyEl.textContent = 'Every queue is empty. A strategy only chooses among queues that have a job waiting.';
		}
		stepBtn.disabled = !next;
		playBtn.disabled = !next && !playing;
		playBtn.textContent = reduced() ? 'Run all' : playing ? 'Pause' : 'Play';
	};
	const render = () => flip(root, paint);

	const step = (): boolean => {
		const p = pick(strategy, queues.map((x) => ({ name: x.name, len: x.jobs.length })), rr);
		if (!p) return false;
		const qu = queues[p.index];
		const n = qu.jobs.shift()!;
		taken.push({ key: key(qu, n), label: `${qu.short} ${n}`, tone: qu.tone });
		say(root, `Took ${qu.short} ${n} from ${qu.name}. ${p.why}`);
		return true;
	};

	const schedule = () => {
		if (!playing) return;
		timers.after(STEP_MS, () => {
			if (!step()) playing = false;
			render();
			schedule();
		});
	};

	stepBtn.addEventListener('click', () => {
		playing = false;
		timers.clear();
		step();
		render();
	});
	playBtn.addEventListener('click', () => {
		if (reduced()) {
			while (step());
			render();
			return;
		}
		playing = !playing;
		timers.clear();
		paint();
		schedule();
	});
	q(root, '[data-reset]').addEventListener('click', () => reset());
	root.querySelectorAll<HTMLElement>('[data-add]').forEach((b) =>
		b.addEventListener('click', () => {
			const qu = queues.find((x) => x.name === b.dataset.add)!;
			qu.jobs.push(++qu.count);
			total++;
			caption(root, `${qu.name}.Add("${qu.short} ${qu.count}")`);
			render();
			say(root, `Added ${qu.short} ${qu.count} to ${qu.name}.`);
		}),
	);

	tabs(q(root, '[role="tablist"]'), (k) => {
		panel.setAttribute('aria-labelledby', `ga-strat-tab-${k}`);
		if (k !== strategy || !queues.length) reset(k as StrategyName);
	});

	reset('Priority');
	root.dataset.ready = '';
}

document.querySelectorAll<HTMLElement>('[data-ga="strategy-picker"]').forEach(init);
