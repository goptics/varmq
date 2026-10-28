// Redis scene. The rules come from the Redis page: one script moves a taken job from the queue to
// <key>:nacked with a deadline in <key>:timeouts (visibility timeout, 5 minutes by default);
// Acknowledge removes both; every queue object sweeps once a second and puts back jobs whose deadline
// passed or whose holder's heartbeat is 30 seconds old; a distributed consumer is taken for dead
// about six seconds after the crash; Close hands jobs back at once.

import { h, job, tabs } from './ops';

type Mode = 'persistent' | 'dist';

const VISIBILITY = 300; // seconds
const HEARTBEAT = 30;
const DIST_BACK = 6;

interface State {
	mode: Mode;
	jobs: number[];
	inflight: number | null;
	closedUnder: boolean;
	crashed: boolean;
	t: number;
	returned: boolean; // the sweep has put the job back
	runs: Record<number, number>;
	msg: string;
}

const root = document.querySelector<HTMLElement>('[data-rflow]');
if (root) init(root);

function init(root: HTMLElement) {
	const figure = root.closest<HTMLElement>('.ops-scene')!;
	const $ = <T extends HTMLElement>(s: string) => figure.querySelector<T>(s)!;
	const procs = $('[data-procs]');
	const keys = $('[data-keys]');
	const clock = $('[data-clock]');
	const clockLabel = $('[data-clock-label]');
	const slider = $<HTMLInputElement>('[data-t]');
	const tOut = $('[data-t-out]');
	const out = $('[data-out]');
	const call = $('[data-call]');
	const btn = (a: string) => $<HTMLButtonElement>(`[data-act="${a}"]`);

	const fresh = (mode: Mode): State => ({
		mode,
		jobs: [7, 8, 9, 10],
		inflight: null,
		closedUnder: false,
		crashed: false,
		t: 0,
		returned: false,
		runs: {},
		msg: 'Job 7 waits in jobs. Press Take a job to see what one script does to the keys.',
	});
	let s = fresh('persistent');

	const mmss = (sec: number) => `${Math.floor(sec / 60)}:${String(sec % 60).padStart(2, '0')}`;
	const threshold = () => (s.mode === 'dist' ? DIST_BACK : HEARTBEAT);
	const held = () => s.inflight !== null && !s.closedUnder && !s.returned;

	function key(name: string, kind: string, ...kids: (Node | null)[]) {
		const body = h('div', { class: 'flow__val' }, ...kids.filter(Boolean));
		if (!body.childElementCount) body.append(h('small', { text: 'empty' }));
		return h('div', { class: 'flow__key' }, h('div', { class: 'flow__keyhead' }, h('code', { text: name }), kind && h('span', { text: kind })), body);
	}
	const entry = (a: string, b: Node | string) => h('span', { class: 'flow__entry' }, h('code', { text: a }), h('i', { text: '→' }), typeof b === 'string' ? h('span', { text: b }) : b);

	function render() {
		const inN = s.inflight;
		const running = s.inflight !== null && !s.returned;
		const dead = s.crashed;
		procs.replaceChildren(
			h(
				'div',
				{ class: 'ack__box', ...(dead ? { 'data-down': true } : {}) },
				h('div', { class: 'ack__head' }, h('span', { text: 'consumer A' }), h('b', { text: dead ? 'died' : running ? 'running a job' : 'idle' })),
				h('div', { class: 'ack__slot' }, running ? job(inN!, dead ? 'is-lost' : 'is-run') : null, h('small', { text: running ? (dead ? 'gone with the process' : `process(job ${inN})`) : dead ? 'down' : 'no job' })),
			),
			...(s.mode === 'dist'
				? [h('div', { class: 'ack__box' }, h('div', { class: 'ack__head' }, h('span', { text: 'consumer B' }), h('b', { text: 'idle, waiting for a wake-up' })), h('div', { class: 'ack__slot' }, h('small', { text: 'no job' })))]
				: []),
		);

		const nacked = held() || (dead && s.inflight !== null && !s.returned);
		const remaining = Math.max(0, VISIBILITY - (dead ? s.t : 0));
		const age = dead ? s.t : 0;
		keys.replaceChildren(
			key('jobs', 'list, front on the left', ...s.jobs.map((id, i) => job(id, s.returned && i === 0 ? 'is-back' : ''))),
			key('jobs:nacked', 'hash', nacked && inN !== null ? entry(`ack-${inN}`, job(inN, dead ? 'is-orphan' : '')) : null),
			key('jobs:timeouts', '', nacked && inN !== null ? entry(`ack-${inN}`, `deadline in ${mmss(remaining)}`) : null),
			key(
				'jobs:heartbeats',
				'',
				s.mode === 'persistent'
					? entry('queue object A', dead ? `last renewed ${age} s ago (taken for dead at ${HEARTBEAT} s)` : 'renewed when it takes jobs and on every sweep')
					: entry('consumer A', dead ? `wake-up channel dropped; others act after ${DIST_BACK - 1} s` : 'wake-up channel open'),
			),
		);

		clock.hidden = !dead || s.inflight === null;
		clockLabel.textContent = s.mode === 'dist' ? 'Time since the crash' : 'Time the program has run since the crash';
		slider.value = String(s.t);
		tOut.textContent = `${s.t} s`;
		slider.max = s.mode === 'dist' ? '12' : '60';

		btn('take').disabled = s.inflight !== null || s.jobs.length === 0 || s.crashed;
		btn('ack').disabled = s.inflight === null || s.crashed || s.returned;
		btn('kill').disabled = s.crashed;
		btn('close').disabled = s.inflight === null || s.crashed || s.closedUnder || s.returned;
		call.textContent = s.crashed
			? '// no Acknowledge was called: the entry stays in jobs:nacked'
			: s.closedUnder
				? 'pq.Close()  // puts back every job this object took and did not acknowledge'
				: s.inflight === null
					? 'pq := rdb.NewQueue("jobs")  // jobs, jobs:nacked, jobs:timeouts, jobs:heartbeats'
					: 'queue.Add / worker takes job  // one script: jobs → jobs:nacked + deadline';

		const tally = Object.entries(s.runs)
			.map(([id, n]) => `job ${id} started ${n}×`)
			.join(', ');
		out.replaceChildren(h('span', { text: s.msg }), ' ', tally && h('span', { class: 'ack__count', text: tally + '.' }));
	}

	function sweepIfDue() {
		if (!s.crashed || s.inflight === null || s.returned) return;
		if (s.t >= threshold()) {
			s.returned = true;
			s.jobs.unshift(s.inflight);
			s.inflight = null;
			s.crashed = false; // a consumer is alive again: the program restarted, or consumer B
			s.msg =
				s.mode === 'dist'
					? 'About six seconds after the crash, Redis has dropped the dead consumer’s wake-up channel, and consumer B’s sweep puts job 7 back at the front of jobs and wakes a consumer for it.'
					: `The restarted program has run for ${HEARTBEAT} seconds. A queue object’s sweep finds the dead object’s heartbeat that old, and puts job 7 back at the front of jobs. A job past its deadline comes back as soon as any queue object finds it, whichever comes first.`;
		} else {
			s.msg =
				s.mode === 'dist'
					? 'Job 7 is still in jobs:nacked. The other consumers wait for the dead one’s channel to go.'
					: `Job 7 is still in jobs:nacked. The heartbeat is ${s.t} seconds old and the limit is ${HEARTBEAT}. The deadline is in ${mmss(Math.max(0, VISIBILITY - s.t))}.`;
		}
	}

	root.closest('.ops-scene')!.addEventListener('click', (e) => {
		const el = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-act]');
		if (!el || el.disabled) return;
		const act = el.dataset.act;
		if (act === 'take') {
			s.inflight = s.jobs.shift()!;
			s.runs[s.inflight] = (s.runs[s.inflight] ?? 0) + 1;
			s.returned = false;
			s.closedUnder = false;
			s.msg = `One script moved job ${s.inflight} from jobs to jobs:nacked under ack-${s.inflight}, and set its deadline in jobs:timeouts: Redis’s time plus the visibility timeout, 5 minutes by default.`;
		} else if (act === 'ack') {
			const id = s.inflight!;
			if (s.closedUnder) {
				s.msg = `Acknowledge returned false: the queue was closed, so the worker reports ErrAcknowledgeJob on Errs(). The function of job ${id} had run, and job ${id} is back in jobs, so it runs again.`;
				s.inflight = null;
				s.closedUnder = false;
			} else {
				s.msg = `Acknowledge removed the entry from jobs:nacked and from jobs:timeouts. Redis forgot job ${id}.`;
				s.inflight = null;
			}
		} else if (act === 'kill') {
			s.crashed = true;
			s.t = 0;
			s.msg =
				s.inflight === null
					? 'The process died while no job was running. Nothing is held. The jobs wait in jobs.'
					: `The process died. Job ${s.inflight} stays in jobs:nacked with its deadline. Move the slider to move time forward.`;
		} else if (act === 'close') {
			s.closedUnder = true;
			s.jobs.unshift(s.inflight!);
			s.msg = `Close put job ${s.inflight} back at the front of jobs at once, while its function still runs. After Close, Acknowledge on that object returns false. Stop the worker before you close the queue.`;
		} else if (act === 'reset') {
			s = fresh(s.mode);
		}
		render();
	});

	slider.addEventListener('input', () => {
		s.t = Number(slider.value);
		sweepIfDue();
		render();
	});

	tabs(
		$('[data-rflow-modes]'),
		(k) => {
			s = fresh(k as Mode);
			render();
		},
		{ shared: root },
	);

	render();
}
