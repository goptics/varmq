// Context scene: what a done context does to running and waiting jobs.
// The model is WorkerSim: the worker follows the context like Stop does, running jobs finish,
// waiting jobs stay queued, and nothing is reported as an error.

import { WorkerSim } from './sim';
import { PoolView } from './poolview';
import { tabs } from '../ui/dom';
import { highlightGo } from '../ui/go';
import { announcer, caller, clock, qs, setText } from './scene';

const DUR = 4000;
const TIMEOUT = 6000;

document.querySelectorAll<HTMLElement>('[data-scene="gb-context"]').forEach(init);

function init(root: HTMLElement) {
	let made: 'cancel' | 'timeout' = 'cancel';
	let watch = false;
	let sim = make();
	const view = new PoolView(qs(root, '[data-pool]'), sim, { maxLanes: 3, legend: ['ok', 'early'] });
	const say = caller(root);
	const live = announcer(root);
	const codeEl = qs(root, '[data-code]');
	const stat = (k: string) => qs(root, `[data-stat="${k}"]`);
	const kv = (k: string) => qs(root, `[data-kv="${k}"]`);
	const watchBtn = qs<HTMLButtonElement>(root, '[data-act="watch"]');
	const cancelBtn = qs<HTMLButtonElement>(root, '[data-act="cancel"]');
	const deadlineRow = qs(root, '[data-deadline]');
	let lastAdd = 'true';
	let lastStatus = '';

	function make() {
		const s = new WorkerSim({ limit: 2 });
		for (let i = 0; i < 5; i++) s.add({ dur: DUR, watch });
		s.advance(40);
		s.onEvent = (e) => view?.record(e);
		return s;
	}

	const code = () => {
		const ctx =
			made === 'cancel'
				? 'ctx, cancel := context.WithCancel(context.Background())'
				: 'ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)';
		const worker = watch
			? `var worker varmq.IWorkerBinder[int]
worker = varmq.NewWorker(func(j varmq.Job[int]) {
	select {
	case <-time.After(4 * time.Second): // the work
	case <-worker.Context().Done(): // return early
	}
}, 2, varmq.WithContext(ctx))`
			: `worker := varmq.NewWorker(process, 2, varmq.WithContext(ctx))`;
		codeEl.innerHTML = highlightGo(`${ctx}\ndefer cancel()\n\n${worker}`);
	};

	const errText = () => (sim.ctx === 'canceled' ? 'context canceled' : sim.ctx === 'deadline' ? 'context deadline exceeded' : 'nil');

	const render = () => {
		view.update();
		setText(stat('status'), sim.status);
		setText(stat('processing'), String(sim.numProcessing()));
		setText(stat('pending'), String(sim.numPending()));
		setText(stat('len'), String(sim.queueLen()));
		setText(kv('err'), errText());
		setText(kv('errs'), 'empty: a cancel is not an error');
		setText(kv('add'), lastAdd);
		deadlineRow.hidden = made !== 'timeout';
		if (made === 'timeout') {
			setText(kv('deadline'), sim.userCtx ? 'passed' : `in ${(Math.max(0, TIMEOUT - sim.now) / 1000).toFixed(1)} s`);
		}
		cancelBtn.disabled = sim.userCtx !== null;
		if (sim.status !== lastStatus) {
			lastStatus = sim.status;
			if (sim.status === 'Stopping') live(`Worker is Stopping. ${sim.numProcessing()} jobs still running, ${sim.numPending()} pending.`);
			else if (sim.status === 'Stopped') live(`Worker is Stopped. ${sim.numPending()} jobs are still pending.`);
			else live(`Worker is ${sim.status}.`);
		}
	};

	const reset = () => {
		sim = make();
		view.reset(sim);
		lastAdd = 'true';
		lastStatus = '';
		code();
		render();
	};

	const clk = clock(root, (dt) => {
		sim.advance(dt);
		if (made === 'timeout' && !sim.userCtx && sim.now >= TIMEOUT) {
			sim.cancelUser('deadline');
			say('// the deadline passed: the context is done, the worker follows');
		}
		render();
	});

	tabs(qs(root, '[data-made]'), (v) => {
		made = v as 'cancel' | 'timeout';
		reset();
		say(made === 'cancel' ? 'context.WithCancel(...) // you call cancel()' : 'context.WithTimeout(..., 6*time.Second) // the deadline cancels it');
	});

	root.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-act]');
		if (!btn || btn.disabled) return;
		const act = btn.dataset.act;
		if (act === 'watch') {
			watch = !watch;
			watchBtn.setAttribute('aria-pressed', String(watch));
			watchBtn.textContent = `Function watches Done(): ${watch ? 'on' : 'off'}`;
			reset();
			say(watch ? '// the function selects on worker.Context().Done()' : '// the function runs to the end, whatever the context does');
		} else if (act === 'cancel') {
			sim.cancelUser('canceled');
			say(watch ? 'cancel() // running jobs see Done() and return early' : 'cancel() // running jobs keep going; waiting jobs do not start');
		} else if (act === 'stop') {
			const err = sim.stop();
			say(`worker.Stop() // ${err ?? 'nil'}: the status is Stopping before Stop returns`);
		} else if (act === 'start') {
			const err = sim.start();
			say(
				err
					? `worker.Start() // ${err}`
					: sim.userCtx
						? 'worker.Start() // nil, but the context is done, so the worker stops again (it may start a job first)'
						: 'worker.Start() // nil',
			);
		} else if (act === 'restart') {
			const err = sim.restart();
			say(
				err
					? `worker.Restart() // ${err}`
					: sim.userCtx
						? 'worker.Restart() // nil, but the context is done, so the worker stops again (it may start a job first)'
						: 'worker.Restart() // nil, with a fresh Context(); parked jobs run first',
			);
		} else if (act === 'add') {
			sim.add({ dur: DUR, watch });
			lastAdd = 'true';
			say(sim.status === 'Stopped' ? 'queue.Add(n) // true: the queue takes it, nothing will run it' : 'queue.Add(n) // true');
		} else if (act === 'reset') {
			reset();
			say('// a new worker with five jobs');
		}
		render();
		clk.wake();
	});

	code();
	render();
}
