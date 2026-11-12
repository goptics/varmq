// Scene: one workflow run on the real state machine (wfmachine.ts, a port of wfmachine.go). Each step, every running
// job ends, then every queued job meets the worker's pre-run check: it starts, or ends as cancelled with the reason
// `workflow cancelled`. Every worker has room for all its tasks, so a step is one round of jobs.
import { caption, q, say } from './kit';
import { h, reducedMotion, tabs, Timers } from '../ui/dom';
import { Machine, type JobState, type WorkflowState } from './wfmachine';
import { EXAMPLES, type Example } from './workflow-plan';

const STEP_MS = 1100;
const FINAL: WorkflowState[] = ['completed', 'failed', 'compensated', 'compensation_failed', 'cancelled'];

function init(root: HTMLElement) {
	const timers = new Timers();
	const dag = q<HTMLElement>(root, '[data-dag]');
	const svg = q<SVGSVGElement>(root, '[data-edges]');
	const failsList = q<HTMLElement>(root, '[data-fails]');
	const note = q<HTMLElement>(root, '[data-note]');
	const failsField = q<HTMLElement>(root, '[data-fails-field]');
	const submitBtn = q<HTMLButtonElement>(root, '[data-submit]');
	const stepBtn = q<HTMLButtonElement>(root, '[data-step]');
	const cancelBtn = q<HTMLButtonElement>(root, '[data-cancel]');
	const ffBtn = q<HTMLButtonElement>(root, '[data-failfast]');
	const alwaysBtn = q<HTMLButtonElement>(root, '[data-always]');
	const wfState = q<HTMLElement>(root, '[data-wf-state]');

	let ex: Example = EXAMPLES[0];
	let m: Machine | null = null;
	let fail = ex.fail;
	let running: string[] = []; // "task" or "task:undo"
	let reasons = new Map<string, string>();
	let playing = false;
	let cancelled = false;

	const pressed = (b: HTMLButtonElement) => b.getAttribute('aria-pressed') === 'true';
	const ended = () => !!m && FINAL.includes(m.state);
	const task = (name: string) => ex.tasks.find((t) => t.name === name)!;

	/** The boxes and the "fails" choice of the example. */
	const build = () => {
		dag.replaceChildren(
			svg,
			...ex.tasks.map((t) =>
				h(
					'section',
					{ class: 'wf-node', attrs: { 'data-node': t.name, 'data-col': String(t.col), 'data-state': 'new', 'aria-label': `Task ${t.name}` } },
					h('h3', {}, t.name, ...(t.always ? [h('small', { text: 'Always', attrs: { 'data-always-tag': '' } })] : [])),
					h('p', { class: 'wf-node__state', text: 'not submitted', attrs: { 'data-st': '' } }),
					...(t.result ? [h('p', { class: 'wf-node__result', attrs: { 'data-res': '' } })] : []),
					...(t.undo ? [h('p', { class: 'wf-node__undo', attrs: { 'data-undo': '' } }, `undo ${t.undo}: `, h('span', { text: '—', attrs: { 'data-ust': '' } }))] : []),
				),
			),
		);
		dag.style.setProperty('--local-cols', String(Math.max(...ex.tasks.map((t) => t.col)) + 1));
		dag.style.setProperty('--local-rows', String(Math.max(...ex.tasks.map((t) => ex.tasks.filter((x) => x.col === t.col).length))));
		failsList.replaceChildren(
			...['none', ...ex.tasks.map((t) => t.name)].map((k) =>
				h('button', { text: k === 'none' ? 'nothing' : k, attrs: { type: 'button', role: 'radio', 'data-key': k, 'aria-checked': String(k === fail), tabindex: k === fail ? '0' : '-1' } }),
			),
		);
		failsField.hidden = ffBtn.hidden = alwaysBtn.hidden = !ex.options;
		note.textContent = ex.note;
	};

	const paint = () => {
		for (const t of ex.tasks) {
			const node = q<HTMLElement>(dag, `[data-node="${t.name}"]`);
			const tk = m?.tasks.get(t.name);
			const st: JobState | 'new' | 'running' = !tk ? 'new' : running.includes(t.name) ? 'running' : tk.state;
			node.dataset.state = st;
			const reason = reasons.get(t.name);
			q(node, '[data-st]').textContent = st === 'new' ? 'not submitted' : reason ? `${st}: ${reason}` : st;
			const res = node.querySelector<HTMLElement>('[data-res]');
			if (res) res.textContent = st === 'completed' ? `result: ${t.result}` : '';
			const u = node.querySelector<HTMLElement>('[data-ust]');
			if (u) {
				// A completed task's undo waits until an undo phase reaches it. No undo runs for a task that did not
				// complete, or after the workflow has ended.
				const idle = tk?.state === 'failed' || tk?.state === 'cancelled' || ended() ? '—' : 'waiting';
				const us = !tk ? '—' : running.includes(`${t.name}:undo`) ? 'running' : tk.undoState || idle;
				u.textContent = us;
				u.parentElement!.dataset.state = us;
			}
			const tag = node.querySelector<HTMLElement>('[data-always-tag]');
			if (tag) tag.hidden = !pressed(alwaysBtn);
		}
		wfState.textContent = m ? `state: ${m.state}` : 'not submitted';
		wfState.dataset.state = m?.state ?? '';
		submitBtn.textContent = !m || ended() ? 'Submit' : playing ? 'Pause' : 'Play';
		submitBtn.disabled = ended();
		stepBtn.disabled = ended();
		cancelBtn.disabled = !m || m.state !== 'running' || cancelled;
		ffBtn.disabled = alwaysBtn.disabled = !!m;
		// The task that fails is fixed at Submit, as the two options are.
		for (const b of failsList.querySelectorAll('button')) b.disabled = !!m;
		edges();
	};

	// The arrows from each dependency to its task, from the boxes' places: left to right, or top to bottom when the
	// scene stacks.
	const edges = () => {
		const box = dag.getBoundingClientRect();
		const at = (name: string) => q<HTMLElement>(dag, `[data-node="${name}"]`).getBoundingClientRect();
		const paths: string[] = [];
		for (const t of ex.tasks) {
			const b = at(t.name);
			for (const d of t.deps ?? []) {
				const a = at(d);
				const side = b.left >= a.right - 1;
				const [x1, y1, x2, y2] = side
					? [a.right, a.top + a.height / 2, b.left, b.top + b.height / 2]
					: [a.left + a.width / 2, a.bottom, b.left + b.width / 2, b.top];
				const [sx, sy, ex2, ey] = [x1 - box.left, y1 - box.top, x2 - box.left, y2 - box.top];
				const c = side ? `C${(sx + ex2) / 2},${sy} ${(sx + ex2) / 2},${ey}` : `C${sx},${(sy + ey) / 2} ${ex2},${(sy + ey) / 2}`;
				const live = m?.tasks.get(d)?.state === 'completed' ? ' is-live' : '';
				paths.push(`<path class="wf-edge${live}" d="M${sx},${sy} ${c} ${ex2},${ey}" marker-end="url(#wf-arrow)"/>`);
			}
		}
		svg.setAttribute('viewBox', `0 0 ${box.width} ${box.height}`);
		svg.innerHTML = `<defs><marker id="wf-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto"><path d="M0,0 8,4 0,8z" class="wf-arrow"/></marker></defs>${paths.join('')}`;
	};

	/** One round: the running jobs end, then the queued ones start or fail the pre-run check. */
	const step = () => {
		if (!m || ended()) return;
		const news: string[] = [];
		for (const key of running) {
			const [name, u] = key.split(':');
			const t = task(name);
			const bad = !u && name === fail;
			if (bad) reasons.set(name, t.error);
			m.finish(name, bad ? 'failed' : 'completed', !!u);
			news.push(u ? `undo ${t.undo} completed` : `${name} ${bad ? `failed: ${t.error}` : 'completed'}`);
		}
		running = [];
		for (let changed = true; changed && !ended(); ) {
			changed = false;
			for (const name of m.order) {
				const t = m.tasks.get(name)!;
				if (t.state === 'pending') {
					changed = true;
					if (m.runnable(name)) {
						t.state = 'processing';
						running.push(name);
					} else {
						reasons.set(name, 'workflow cancelled');
						m.finish(name, 'cancelled');
						news.push(`${name} cancelled at the pre-run check`);
					}
				}
				if (t.undoState === 'pending' && m.runnable(name, true)) {
					t.undoState = 'processing';
					running.push(`${name}:undo`);
				}
			}
		}
		if (running.length) news.push(`running: ${running.map((k) => k.replace(':undo', ' undo')).join(', ')}`);
		if (ended()) {
			playing = false;
			caption(root, `rec, err := wf.Wait() // rec.State == "${m.state}"`);
			news.push(`The workflow ended as ${m.state}.`);
		} else if (m.state === 'compensating') caption(root, '// compensating: undo jobs run in reverse order');
		paint();
		say(root, news.join('. '));
	};

	const loop = () => {
		if (!playing) return;
		step();
		if (playing) timers.after(STEP_MS, loop);
	};

	const submit = () => {
		const always = pressed(alwaysBtn);
		const ff = ex.options && pressed(ffBtn);
		m = new Machine(
			ex.tasks.map((t) => ({ name: t.name, deps: t.deps, undo: !!t.undo, always: t.always && (always || !ex.options) })),
			ff,
		);
		caption(root, ff ? 'wf := varmq.NewWorkflow(store, varmq.WithFailFast())\nwf.Submit(ctx)' : 'wf.Submit(ctx)');
		paint();
		say(root, `Submitted. ${ex.tasks[0].name} has no dependencies, so it is queued.`);
	};

	const reset = () => {
		timers.clear();
		m = null;
		running = [];
		reasons = new Map();
		playing = false;
		cancelled = false;
		caption(root, ex.call);
		paint();
		say(root, 'Reset. Nothing is submitted.');
	};

	submitBtn.addEventListener('click', () => {
		if (!m) submit();
		if (reducedMotion()) {
			while (!ended()) step();
			return;
		}
		playing = !playing;
		paint();
		if (playing) timers.after(STEP_MS / 2, loop);
		else timers.clear();
	});
	stepBtn.addEventListener('click', () => {
		playing = false;
		timers.clear();
		if (!m) submit();
		else step();
	});
	cancelBtn.addEventListener('click', () => {
		if (!m) return;
		cancelled = true;
		m.cancel();
		caption(root, 'wf.Cancel(ctx) // waiting tasks: cancelled now; queued: at the pre-run check');
		paint();
		say(root, 'Cancelled. Waiting tasks are cancelled. Running tasks finish. Then the undo jobs of completed tasks run.');
	});
	ffBtn.addEventListener('click', () => {
		ffBtn.setAttribute('aria-pressed', String(!pressed(ffBtn)));
		paint();
	});
	alwaysBtn.addEventListener('click', () => {
		alwaysBtn.setAttribute('aria-pressed', String(!pressed(alwaysBtn)));
		paint();
	});
	q(root, '[data-reset]').addEventListener('click', reset);
	tabs(failsList, (k) => {
		fail = k;
		say(root, k === 'none' ? 'No task fails.' : `The function of ${k} returns an error.`);
	});
	tabs(q(root, '[data-examples]'), (k) => {
		ex = EXAMPLES.find((e) => e.key === k)!;
		fail = ex.fail;
		ffBtn.setAttribute('aria-pressed', 'false');
		alwaysBtn.setAttribute('aria-pressed', 'true');
		build();
		reset();
		say(root, `${ex.label} example. ${ex.note}`);
	});

	new ResizeObserver(() => edges()).observe(dag);
	root.dataset.ready = '';
	build();
	paint();
}

document.querySelectorAll<HTMLElement>('[data-scene="workflow-run"]').forEach(init);
