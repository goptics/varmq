// Crash test scene. One job (7) goes through the acknowledgement steps of the overview page. The
// rules follow the pages: varmq acknowledges after the function returns, and what happens to a job
// that was taken and not acknowledged is the adapter's choice (redisq, sqliteq, or your own).

import { h, setCall, tabs } from '../ui/dom';
import { stepsOf, type Mode } from './ack-data';
import { job, refocus } from './ops';

interface State {
	mode: Mode;
	stage: number; // index into stepsOf(mode)
	down: boolean; // the process is dead
	crashStage: number; // stage at the crash, -1 when none
	orphan: boolean; // the storage holds job 7 as taken, and nothing will acknowledge it
	lost: boolean; // job 7 is gone
	othersLost: boolean; // jobs 8, 9 and 10 are gone
	started: number; // runs of job 7's function
	msg: string;
}

const STORE_NAME: Record<Mode, string> = {
	memory: '',
	redis: 'Redis (redisq)',
	sqlite: 'SQLite (sqliteq)',
	own: 'Your storage',
};
const HINT: Record<Mode, [string, string, string]> = {
	memory: ['', '', ''],
	redis: ['jobs', 'jobs:nacked', 'entry removed'],
	sqlite: ['pending', 'processing', 'row deleted'],
	own: ['Enqueue', 'DequeueWithAckId', 'Acknowledge true'],
};

const root = document.querySelector<HTMLElement>('[data-ack]');
if (root) init(root);

function init(root: HTMLElement) {
	const figure = root.closest<HTMLElement>('.sframe')!;
	const $ = <T extends HTMLElement>(s: string) => figure.querySelector<T>(s)!;
	const procs = $('[data-procs]');
	const store = $('[data-store]');
	const wire = $('.ack__wire');
	const storeName = $('[data-store-name]');
	const zones = $('[data-zones]');
	const stepsEl = $('[data-steps]');
	const out = $('[data-out]');
	const call = $('[data-call]');
	const runs = $('[data-stat="runs"]');
	const acked = $('[data-stat="acked"]');
	const btn = (a: string) => $<HTMLButtonElement>(`[data-act="${a}"]`);

	const fresh = (mode: Mode): State => ({
		mode,
		stage: 0,
		down: false,
		crashStage: -1,
		orphan: false,
		lost: false,
		othersLost: false,
		started: 0,
		msg: 'Press Step to move job 7 along, and Kill the process at any step.',
	});
	let s = fresh('redis');

	const persistent = () => s.mode !== 'memory';
	const last = () => stepsOf(s.mode).length - 1;
	/** Said when job 7 comes back: whether its function will run for the first or the second time. */
	const again = () => (s.crashStage >= 2 ? ' Its function will run a second time.' : ' Its function has not run yet.');

	function processBox() {
		const dead = s.down;
		// Job 7 is in the process from Taken until the storage hears Acknowledge (until Close in memory).
		const inSlot = s.stage >= 1 && s.stage <= (persistent() ? 3 : 2) && ((!s.lost && !s.orphan) || dead);
		const head = h('div', { class: 'ack__head' }, h('span', { text: 'process' }), h('b', { text: dead ? 'died' : inSlot ? 'busy' : 'idle' }));
		const slot = h('div', { class: 'ack__slot' });
		if (inSlot) {
			const cls = dead ? 'is-lost' : s.stage === 2 ? 'is-run' : s.stage === 3 ? 'is-done' : '';
			slot.append(job(7, cls), h('small', { text: dead ? 'gone with the process' : ['', persistent() ? 'got ack-7' : 'taken', 'running', 'returned'][s.stage] }));
		} else {
			slot.append(h('small', { text: dead ? 'down' : 'no job' }));
		}
		const el = h('div', { class: 'ack__box', ...(dead ? { 'data-down': true } : {}) }, head, slot);
		if (!persistent()) {
			const q = h('div', { class: 'ack__q' });
			const ids = s.othersLost ? [] : [...(s.stage === 0 && (!s.lost || dead) ? [7] : []), 8, 9, 10];
			for (const id of ids) q.append(job(id, dead ? 'is-lost' : ''));
			if (!q.childElementCount) q.append(h('small', { text: 'empty' }));
			el.append(h('div', { class: 'ack__zlabel' }, h('span', { text: 'in memory' })), q);
		}
		return el;
	}

	function zone(label: string, hint: string, chips: Node[], none = 'empty') {
		const body = h('div', { class: 'ack__q' }, ...chips);
		if (!chips.length) body.append(h('small', { text: none }));
		return h('div', { class: 'ack__zone' }, h('div', { class: 'ack__zlabel' }, h('span', { text: label }), hint && h('code', { text: hint })), body);
	}

	function render() {
		const [hw, ht, hd] = HINT[s.mode];
		procs.replaceChildren(processBox());
		// In memory nothing lives outside the process, so there is no storage to draw.
		store.hidden = !persistent();
		wire.style.display = persistent() ? '' : 'none';
		if (persistent()) {
			storeName.textContent = STORE_NAME[s.mode];
			const waiting: Node[] = [];
			if (s.stage === 0 && !s.lost) waiting.push(job(7));
			if (!s.othersLost) for (const id of [8, 9, 10]) waiting.push(job(id));
			const ran = s.orphan && s.crashStage === 3 ? ' · ran 1×' : '';
			const taken = s.stage >= 1 && s.stage <= 3 && !s.lost ? [job(7, s.down || s.orphan ? 'is-orphan' : ''), h('small', { class: 'ack__ack', text: `ack-7${ran}` })] : [];
			zones.replaceChildren(
				zone('waiting', hw, waiting),
				zone('taken, not acknowledged', ht, taken),
				zone('acknowledged', hd, [], s.stage === 4 ? 'job 7 dropped' : 'none yet'),
			);
		}

		const steps = stepsOf(s.mode);
		stepsEl.style.setProperty('--n', String(steps.length));
		stepsEl.replaceChildren(
			...steps.map((st, i) =>
				h(
					'li',
					{
						class: `ack__step${i < s.stage ? ' is-past' : ''}${i === s.stage && !s.lost ? ' is-now' : ''}${persistent() && i === 3 ? ' is-warn' : ''}`,
						...(i === s.stage && !s.lost ? { 'aria-current': 'step' } : {}),
					},
					h('span', { class: 'ack__stepn', text: String(i + 1) }),
					h('span', { class: 'ack__stept', text: st.label }),
					h('code', { class: 'ack__sub', text: st.sub }),
				),
			),
		);

		runs.textContent = String(s.started);
		runs.dataset.tone = s.started >= 2 ? 'bad' : '';
		acked.textContent = !persistent() ? 'nothing to acknowledge' : s.stage === 4 ? 'yes' : 'no';

		btn('step').disabled = s.down || s.stage >= last() || s.orphan || s.lost;
		btn('kill').hidden = s.down;
		btn('restart').hidden = !s.down;
		const wait = btn('wait');
		const lose = btn('lose');
		wait.hidden = !(s.orphan && !s.down && s.mode !== 'sqlite');
		lose.hidden = !(s.orphan && !s.down && s.mode === 'own');
		wait.textContent = s.mode === 'redis' ? 'Run for 30 seconds' : 'Hand it out again';
		setCall(
			call,
			!s.down
				? steps[s.stage].call
				: !persistent()
					? '// the process died, and its memory with it'
					: s.stage === 4
						? '// the process died after Acknowledge'
						: '// the process died before Acknowledge reached the storage',
		);
		out.textContent = s.msg;
	}

	function step() {
		if (btn('step').disabled) return;
		s.stage += 1;
		if (s.stage === 1) {
			s.msg = persistent() ? 'The storage hands out job 7 with ack-7 and marks it taken.' : 'The worker takes job 7 from memory. Nothing outside the process records it.';
		} else if (s.stage === 2) {
			s.started += 1;
			s.msg = s.started >= 2 ? 'Job 7 runs a second time.' : 'Your function runs job 7.';
		} else if (s.stage === 3) {
			s.msg = persistent() ? 'The function returned. Acknowledge has not reached the storage yet.' : 'The function returned and the worker closes job 7.';
		} else {
			s.msg = 'Acknowledge(ack-7) returned true. The storage drops job 7.';
		}
		render();
	}

	function kill() {
		if (s.down) return;
		s.down = true;
		s.crashStage = s.stage;
		if (!persistent()) {
			s.lost = s.stage < 3;
			s.msg = s.lost ? 'The process died. The queue was in memory: jobs 7, 8, 9 and 10 are lost.' : 'The process died. Job 7 had finished, but 8, 9 and 10 were in memory and are lost.';
		} else {
			s.orphan = s.stage >= 1 && s.stage <= 3;
			s.msg = [
				'The process died. Jobs 7, 8, 9 and 10 wait in storage.',
				'The process died after taking job 7. The storage holds it as taken, not acknowledged.',
				'The process died while job 7 ran. It is taken, not acknowledged.',
				'The process died after the function returned. Job 7 ran, but it is not acknowledged.',
				'The process died after the acknowledgement. Nothing is lost.',
			][s.stage];
		}
		render();
	}

	function restart() {
		if (!s.down) return;
		s.down = false;
		if (!persistent()) {
			s.stage = 0;
			s.lost = true;
			s.othersLost = true;
			s.msg = 'The process is back with an empty queue. Nothing survived.';
		} else if (s.orphan) {
			if (s.mode === 'sqlite') {
				s.orphan = false;
				s.stage = 0;
				s.msg = `sqliteq opens the queue and sets every processing row back to pending. Job 7 is at the front.${again()}`;
			} else if (s.mode === 'redis') {
				s.msg = 'redisq keeps job 7 in jobs:nacked until the program has run 30 s in all, or until the 5-minute deadline.';
			} else {
				s.msg = 'varmq cannot bring job 7 back. Your adapter decides when, or whether, to hand it out again.';
			}
		} else {
			s.msg = 'The process is back and starts on the jobs in storage.';
		}
		render();
	}

	function wait() {
		if (!s.orphan) return;
		s.orphan = false;
		s.stage = 0;
		s.msg = `${s.mode === 'redis' ? 'redisq puts job 7 back at the front of the queue.' : 'Your adapter hands job 7 out again.'}${again()}`;
		render();
	}

	function lose() {
		if (!s.orphan) return;
		s.orphan = false;
		s.lost = true;
		s.stage = 0;
		s.msg = 'Your adapter never hands job 7 out again. It is lost, and varmq does not retry it.';
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
