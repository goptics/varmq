// Metrics scene: the four counters of Metrics() and the equation Submitted == NumPending +
// NumProcessing + Completed, with the cases where the equation does not hold.

import { WorkerSim } from './sim';
import { PoolView } from './poolview';
import { announcer, caller, clock, qs, radios, setText } from './scene';

const DUR = 2200;

document.querySelectorAll<HTMLElement>('[data-scene="metrics"]').forEach(init);

function init(root: HTMLElement) {
	let limit = 2;
	let purged = 0;
	let resetUsed = false;
	let sim = make();
	const view = new PoolView(qs(root, '[data-pool]'), sim, { maxLanes: 2, legend: ['ok', 'fail', 'closed'] });
	const say = caller(root);
	const live = announcer(root);
	const stat = (k: string) => qs(root, `[data-stat="${k}"]`);
	const kv = (k: string) => qs(root, `[data-kv="${k}"]`);
	const calc = qs(root, '[data-calc]');
	const tag = qs(root, '[data-tag]');
	const why = qs(root, '[data-why]');
	const pauseBtn = qs<HTMLButtonElement>(root, '[data-act="pause-toggle"]');
	let lastHolds = true;

	function make() {
		const s = new WorkerSim({ limit });
		s.onEvent = (e) => view?.record(e);
		return s;
	}

	const render = () => {
		view.update();
		const m = sim.m;
		setText(stat('status'), sim.status);
		setText(stat('pending'), String(sim.numPending()));
		setText(stat('processing'), String(sim.numProcessing()));
		setText(kv('submitted'), String(m.submitted));
		setText(kv('completed'), String(m.completed));
		setText(kv('successful'), String(m.successful));
		setText(kv('failed'), String(m.failed));

		const right = sim.numPending() + sim.numProcessing() + m.completed;
		const holds = m.submitted === right;
		setText(calc, `${m.submitted} == ${sim.numPending()} + ${sim.numProcessing()} + ${m.completed}`);
		setText(tag, holds ? 'holds' : 'does not hold');
		tag.dataset.tone = holds ? 'ok' : 'warn';

		let text = 'Use the equation on a worker with nothing running.';
		if (!holds) {
			if (resetUsed) text = 'Reset zeroed Submitted and Completed. The jobs that still wait or run count on the right, and no longer on the left.';
			else if (purged > 0) text = `Purge closed ${purged} ${purged === 1 ? 'job' : 'jobs'}. ${purged === 1 ? 'It was' : 'They were'} counted in Submitted and never ran, so ${purged === 1 ? 'it' : 'they'} never reach Completed.`;
			else if (sim.curProcessing > sim.numProcessing()) text = 'One goroutine holds a batch. NumPending still counts the jobs of the batch that have finished, until the whole batch ends.';
			else text = 'The four reads were not one snapshot.';
		} else if (sim.numProcessing() > 0) {
			text = 'Jobs are running. At limit 1 a batch can make this read wrong while it runs.';
		}
		setText(why, text);
		why.dataset.tone = holds ? '' : 'warn';

		const paused = sim.status === 'Paused' || sim.status === 'Pausing';
		setText(pauseBtn, paused ? 'Resume()' : 'Pause()');

		if (holds !== lastHolds) {
			lastHolds = holds;
			live(holds ? 'The equation holds again.' : `The equation does not hold. ${text}`);
		}
	};

	const rebuild = () => {
		sim = make();
		view.reset(sim);
		purged = 0;
		resetUsed = false;
		lastHolds = true;
		render();
	};

	const clk = clock(root, (dt) => {
		sim.advance(dt);
		render();
	});

	radios(qs(root, '[data-limit]'), (v) => {
		limit = Number(v);
		rebuild();
		say(`varmq.NewErrWorker(process, ${limit}) // a new worker, counters at 0`);
	});

	root.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLElement>('[data-act]');
		if (!btn) return;
		switch (btn.dataset.act) {
			case 'add':
				sim.add({ dur: DUR });
				say('queue.Add(n) // Submitted() + 1');
				break;
			case 'fail':
				sim.add({ dur: DUR, fail: 'error' });
				say('queue.Add(n) // the function returns an error: Failed() + 1 when it ends');
				break;
			case 'add5':
				for (let i = 0; i < 5; i++) sim.add({ dur: DUR });
				say('queue.Add(n) // 5 times: Submitted() + 5');
				break;
			case 'pause-toggle': {
				const paused = sim.status === 'Paused' || sim.status === 'Pausing';
				const err = paused ? sim.resume() : sim.pause();
				say(`worker.${paused ? 'Resume' : 'Pause'}() // ${err ?? 'nil'}; the counters keep their values`);
				break;
			}
			case 'purge': {
				const n = sim.purge();
				purged += n;
				say(`queue.Purge() // closes ${n} waiting ${n === 1 ? 'job' : 'jobs'}; Submitted() stays`);
				break;
			}
			case 'reset-metrics':
				sim.resetMetrics();
				resetUsed = true;
				say('worker.Metrics().Reset() // all four counters are 0; the queue and running jobs are untouched');
				break;
		}
		render();
		clk.wake();
	});

	render();
}
