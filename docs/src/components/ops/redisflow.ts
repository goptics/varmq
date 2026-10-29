// Redis scene: draws the keys of a queue named jobs. The state and the rules each button follows are in
// redisflow-model.ts.

import { h, setCall, tabs } from '../ui/dom';
import { job, refocus } from './ops';
import { DIST_BACK, HEARTBEAT, VISIBILITY, ack, close, fresh, kill, mmss, nacked, sweepIfDue, take, type Mode } from './redisflow-model';

const root = document.querySelector<HTMLElement>('[data-rflow]');
if (root) init(root);

function init(root: HTMLElement) {
	const figure = root.closest<HTMLElement>('.sframe')!;
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

	let s = fresh('persistent');

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

		const inNacked = nacked(s);
		const remaining = Math.max(0, VISIBILITY - (dead ? s.t : 0));
		const age = dead ? s.t : 0;
		keys.replaceChildren(
			key('jobs', 'list, front on the left', ...s.jobs.map((id, i) => job(id, s.returned && i === 0 ? 'is-back' : ''))),
			key('jobs:nacked', 'hash', inNacked && inN !== null ? entry(`ack-${inN}`, job(inN, dead ? 'is-orphan' : '')) : null),
			key('jobs:timeouts', '', inNacked && inN !== null ? entry(`ack-${inN}`, `deadline in ${mmss(remaining)}`) : null),
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
		setCall(
			call,
			s.crashed
				? s.inflight === null
					? '// the process died holding no job'
					: '// no Acknowledge was called: the entry stays in jobs:nacked'
				: s.closedUnder
					? 'pq.Close()  // puts back every job this object took and did not acknowledge'
					: s.inflight === null
						? 'pq := rdb.NewQueue("jobs")  // jobs, jobs:nacked, jobs:timeouts, jobs:heartbeats'
						: 'queue.Add / worker takes job  // one script: jobs → jobs:nacked + deadline',
		);

		const tally = Object.entries(s.runs)
			.map(([id, n]) => `job ${id} started ${n}×`)
			.join(', ');
		out.replaceChildren(h('span', { text: s.msg }), ' ', tally && h('span', { class: 'ack__count', text: tally + '.' }));
	}

	figure.addEventListener('click', (e) => {
		const el = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-act]');
		if (!el || el.disabled) return;
		const keyboard = el.matches(':focus-visible');
		const act = el.dataset.act;
		if (act === 'take') take(s);
		else if (act === 'ack') ack(s);
		else if (act === 'kill') kill(s);
		else if (act === 'close') close(s);
		else if (act === 'reset') s = fresh(s.mode);
		render();
		if (keyboard) refocus(figure, el);
	});

	slider.addEventListener('input', () => {
		s.t = Number(slider.value);
		sweepIfDue(s);
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
