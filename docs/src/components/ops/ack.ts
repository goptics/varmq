// Crash test scene. One job (7) goes through the acknowledgement steps of the overview page. The
// rules follow the pages: varmq acknowledges after the function returns, and what happens to a job
// that was taken and not acknowledged is the adapter's choice (redisq, sqliteq, or your own).

import { h, setCall, tabs } from '../ui/dom';
import { stepsOf, type Mode } from './ack-data';
import { job, refocus } from './ops';

interface State {
	mode: Mode;
	stage: number; // 0 waiting, 1 taken, 2 running, 3 returned, 4 acknowledged
	runner: 'A' | 'B';
	down: boolean; // process A is dead
	crashStage: number; // stage at the crash, -1 when none
	orphan: boolean; // the storage holds job 7 as taken, and nothing will acknowledge it
	lost: boolean; // job 7 is gone
	othersLost: boolean; // jobs 8, 9 and 10 are gone
	started: number;
	returned: number;
	msg: string;
}

const STORE_NAME: Record<Mode, string> = {
	memory: 'Storage',
	redis: 'Redis (redisq)',
	dist: 'Redis (redisq)',
	sqlite: 'SQLite (sqliteq)',
	own: 'Your storage',
};
const HINT: Record<Mode, [string, string, string]> = {
	memory: ['', '', ''],
	redis: ['jobs', 'jobs:nacked', 'entry removed'],
	dist: ['jobs', 'jobs:nacked', 'entry removed'],
	sqlite: ['pending', 'processing', 'row deleted'],
	own: ['Dequeue', 'IAcknowledgeable', 'Acknowledge true'],
};

const root = document.querySelector<HTMLElement>('[data-ack]');
if (root) init(root);

function init(root: HTMLElement) {
	const figure = root.closest<HTMLElement>('.sframe')!;
	const $ = <T extends HTMLElement>(s: string) => figure.querySelector<T>(s)!;
	const procs = $('[data-procs]');
	const store = $('[data-store]');
	const storeName = $('[data-store-name]');
	const storeTag = $('[data-store-tag]');
	const zones = $('[data-zones]');
	const stepsEl = $('[data-steps]');
	const out = $('[data-out]');
	const call = $('[data-call]');
	const panic = $<HTMLInputElement>('[data-panic]');
	const btn = (a: string) => $<HTMLButtonElement>(`[data-act="${a}"]`);

	const fresh = (mode: Mode): State => ({
		mode,
		stage: 0,
		runner: 'A',
		down: false,
		crashStage: -1,
		orphan: false,
		lost: false,
		othersLost: false,
		started: 0,
		returned: 0,
		msg: 'Press Step to move job 7 through the five steps. Press Kill the process to stop the process at the current step.',
	});
	let s = fresh('redis');

	const persistent = () => s.mode !== 'memory';

	const chip = (id: number, cls = '') => job(id, cls);
	const lostChip = (id: number) => chip(id, 'is-lost');

	function box(who: 'A' | 'B') {
		const dead = who === 'A' && s.down;
		// A job taken by a process that died is in the storage only: no live process holds it.
		const holds = s.stage >= 1 && s.stage <= 3 && s.runner === who && !(s.lost && !s.down) && (!s.orphan || dead);
		const state = dead ? 'died' : holds ? 'busy' : 'idle';
		const head = h('div', { class: 'ack__head' }, h('span', { text: `process ${who}` }), h('b', { text: state }));
		const slot = h('div', { class: 'ack__slot' });
		if (holds) {
			const cls = dead ? 'is-lost' : s.stage === 2 ? 'is-run' : s.stage === 3 ? 'is-done' : '';
			slot.append(chip(7, cls), h('small', { text: dead ? 'gone with the process' : ['', 'got ack-7', 'running', 'returned', ''][s.stage] }));
		} else {
			slot.append(h('small', { text: dead ? 'down' : 'no job' }));
		}
		const el = h('div', { class: 'ack__box', ...(dead ? { 'data-down': true } : {}) }, head, slot);
		if (s.mode === 'memory' && who === 'A') {
			const q = h('div', { class: 'ack__q' });
			const ids = s.othersLost ? [] : [...(s.stage === 0 && !s.lost ? [7] : []), 8, 9, 10];
			for (const id of ids) q.append(chip(id, s.down ? 'is-lost' : ''));
			if (!q.childElementCount) q.append(h('small', { text: 'queue is empty' }));
			el.append(h('div', { class: 'ack__zlabel' }, h('span', { text: 'queue in memory' })), q);
		}
		return el;
	}

	function zone(label: string, hint: string, ...chips: Node[]) {
		const body = h('div', { class: 'ack__q' }, ...chips);
		if (!chips.length) body.append(h('small', { text: 'empty' }));
		return h('div', { class: 'ack__zone' }, h('div', { class: 'ack__zlabel' }, h('span', { text: label }), hint && h('code', { text: hint })), body);
	}

	function render() {
		const [hw, ht, hd] = HINT[s.mode];
		procs.replaceChildren(box('A'), ...(s.mode === 'dist' ? [box('B')] : []));
		const stored = persistent();
		store.toggleAttribute('data-off', !stored);
		storeName.textContent = STORE_NAME[s.mode];
		storeTag.textContent = stored ? 'outside the process' : 'none';
		if (!stored) {
			zones.replaceChildren(h('p', { class: 'ack__none', text: 'Nothing is kept outside the process.' }));
		} else {
			const waiting: Node[] = [];
			if (s.stage === 0 && !s.lost) waiting.push(chip(7));
			if (!s.othersLost) for (const id of [8, 9, 10]) waiting.push(chip(id));
			const taken = s.stage >= 1 && s.stage <= 3 && !s.lost ? [chip(7, s.down || s.orphan ? 'is-orphan' : ''), h('small', { class: 'ack__ack', text: 'ack-7' })] : [];
			const done = s.stage === 4 ? [chip(7, 'is-done')] : [];
			zones.replaceChildren(
				zone('waiting', hw, ...waiting),
				zone('taken, not acknowledged', ht, ...taken),
				zone('acknowledged', hd, ...done),
			);
		}

		const steps = stepsOf(s.mode);
		stepsEl.replaceChildren(
			...steps.map((st, i) =>
				h(
					'li',
					{ class: `ack__step${i < s.stage ? ' is-past' : ''}${i === s.stage && !s.lost ? ' is-now' : ''}`, ...(i === s.stage && !s.lost ? { 'aria-current': 'step' } : {}) },
					h('span', { class: 'ack__stepn', text: String(i + 1) }),
					h('span', { class: 'ack__stept', text: st.label }),
					h('code', { class: 'ack__sub', text: st.sub }),
				),
			),
		);

		const runnerUp = s.mode === 'dist' && s.runner === 'B';
		btn('step').disabled = (s.down && !runnerUp) || s.stage >= 4 || s.orphan || s.lost;
		btn('kill').hidden = s.down;
		btn('restart').hidden = !s.down;
		const wait = btn('wait');
		const lose = btn('lose');
		wait.hidden = true;
		lose.hidden = true;
		if (s.orphan) {
			if (s.mode === 'dist') {
				wait.hidden = false;
				wait.textContent = 'Wait 6 seconds';
			} else if (s.mode === 'redis' && !s.down) {
				wait.hidden = false;
				wait.textContent = 'Run for 30 seconds';
			} else if (s.mode === 'own' && !s.down) {
				wait.hidden = false;
				wait.textContent = 'Hand it out again';
				lose.hidden = false;
			}
		}
		setCall(call, !s.down ? steps[s.stage].call : persistent() ? '// the process died: no Acknowledge was called' : '// the process died, and the queue in memory with it');
		out.innerHTML = '';
		const counts = `Function of job 7: started ${s.started}×, returned ${s.returned}×.`;
		out.append(h('span', { text: s.msg }), ' ', h('span', { class: 'ack__count', text: counts }));
	}

	function step() {
		if (btn('step').disabled) return;
		s.stage += 1;
		if (s.mode === 'dist' && s.runner === 'A' && s.down) s.runner = 'B';
		if (s.stage === 1) {
			s.msg = persistent()
				? 'The storage hands over job 7 and an acknowledgement ID, ack-7. It keeps its own record that job 7 is out.'
				: 'The worker takes job 7 from the queue in memory. There is no acknowledgement ID. Nothing outside the process records that job 7 is out.';
		} else if (s.stage === 2) {
			s.started += 1;
			s.msg = 'Your function runs.';
		} else if (s.stage === 3) {
			s.returned += 1;
			s.msg = panic.checked
				? `The function panicked. varmq recovers the panic, counts the job as failed and reports it. ${persistent() ? 'The next step still acknowledges the job.' : 'Then the worker closes the job.'}`
				: persistent()
					? 'The function returned. The worker closes the job, and closing it calls Acknowledge with ack-7.'
					: 'The function returned. The worker closes the job. In memory there is nothing to acknowledge.';
		} else if (s.stage === 4) {
			s.msg = persistent()
				? `Acknowledge returned true. The storage forgets job 7.${panic.checked ? ' varmq does not retry a job that failed.' : ''}`
				: 'The job is done.';
		}
		render();
	}

	function kill() {
		if (s.down) return;
		s.down = true;
		s.crashStage = s.stage;
		const at = stepsOf(s.mode)[s.stage].label.toLowerCase();
		if (!persistent()) {
			const ran = s.stage >= 3;
			s.lost = !ran;
			s.msg = ran
				? 'The process died after the function returned. Job 7 had run. Waiting jobs 8, 9 and 10 are lost with the queue.'
				: `The process died at step ${s.stage + 1} (${at}). A queue in memory ends with the process, so job 7 is lost, and so are waiting jobs 8, 9 and 10.`;
		} else {
			s.orphan = s.stage >= 1 && s.stage <= 3;
			// Process B carries on. A job A had taken reaches B only when it is put back (wait).
			if (s.mode === 'dist' && !s.orphan) s.runner = 'B';
			s.msg = [
				'The process died while job 7 waited. It is still in storage, with 8, 9 and 10.',
				'The process died after it took job 7. The storage holds job 7 as taken and not acknowledged.',
				'The process died while the function ran. The storage holds job 7 as taken and not acknowledged.',
				'The function had returned, but Acknowledge was never called. The storage holds job 7 as taken and not acknowledged, though it already ran.',
				'The process died after the acknowledgement. Nothing is lost: job 7 was done, and 8, 9 and 10 wait in storage.',
			][s.stage];
			if (s.mode === 'dist' && s.orphan) s.msg += ' Another consumer, process B, is alive.';
		}
		render();
	}

	function restart() {
		if (!s.down) return;
		s.down = false;
		// A job process B runs stays with B: the restarted process A does not take it over.
		if (!persistent()) {
			s.stage = 0;
			s.lost = true;
			s.othersLost = true;
			s.msg = 'The process is back. The queue is empty. Nothing from the earlier run survived. Press Reset to start again.';
		} else if (s.orphan) {
			if (s.mode === 'sqlite') {
				s.orphan = false;
				s.stage = 0;
				s.msg =
					'sqliteq sets every row that is processing back to pending when a process opens the queue. Job 7 is back at the front. There is no timeout. Press Step to run it again.';
			} else if (s.mode === 'redis') {
				s.msg =
					'redisq keeps job 7 in jobs:nacked with a deadline. It puts job 7 back at the front of the queue once the program has run for 30 seconds in all, or at the deadline (5 minutes by default), whichever comes first.';
			} else if (s.mode === 'dist') {
				s.msg = 'The process is back, but job 7 is still held as taken. Another consumer puts it back about six seconds after the crash.';
			} else {
				s.msg = 'varmq cannot bring job 7 back. Your adapter decides: hand it out again after a timeout, at the next start, or never.';
			}
		} else {
			s.msg = 'The process is back. It starts on the jobs in the storage.';
		}
		render();
	}

	function wait() {
		if (!s.orphan) return;
		s.orphan = false;
		s.stage = 0;
		s.runner = 'A';
		if (s.mode === 'dist') {
			s.runner = 'B';
			s.msg = 'About six seconds after the crash, the other consumers put job 7 back at the front of the queue. Process B takes it next. Press Step.';
		} else if (s.mode === 'redis') {
			s.msg = 'The program has run for 30 seconds. redisq puts job 7 back at the front of the queue. Press Step to run it again.';
		} else {
			s.msg = 'This adapter handed job 7 out again. Press Step to run it again.';
		}
		render();
	}

	function lose() {
		if (!s.orphan) return;
		s.orphan = false;
		s.lost = true;
		s.stage = 0;
		s.msg = 'This adapter never hands job 7 out again. The job is lost, and nothing in varmq retries it. Press Reset to start again.';
		render();
	}

	figure.addEventListener('click', (e) => {
		const el = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-act]');
		if (!el) return;
		const keyboard = el.matches(':focus-visible');
		const act = el.dataset.act;
		if (act === 'step') step();
		else if (act === 'kill') kill();
		else if (act === 'restart') restart();
		else if (act === 'wait') wait();
		else if (act === 'lose') lose();
		else if (act === 'reset') {
			s = fresh(s.mode);
			render();
		}
		if (keyboard) refocus(figure, el);
	});

	tabs(
		$('[data-ack-modes]'),
		(key) => {
			s = fresh(key as Mode);
			render();
		},
		{ shared: root },
	);

	render();
}
