// Idle goroutine scene: the limit, WithMinIdleWorkerRatio and WithIdleWorkerExpiryDuration decide
// how many goroutines stay in the pool after a burst. A change creates a new worker.

import { WorkerSim } from './sim';
import { PoolView } from './poolview';
import { reducedMotion, tabs } from '../ui/dom';
import { highlightGo } from '../ui/go';
import { announcer, caller, clock, qs, setText, settle } from './scene';

const DUR = 1600;
const EXPIRY = 4000;

document.querySelectorAll<HTMLElement>('[data-scene="gb-idle"]').forEach(init);

function init(root: HTMLElement) {
	let limit = 6;
	let ratio = 0;
	let expiry = false;
	let sim = make();
	const view = new PoolView(qs(root, '[data-pool]'), sim, { showDone: false, blocks: true });
	const say = caller(root);
	const live = announcer(root);
	const codeEl = qs(root, '[data-code]');
	const limitIn = qs<HTMLInputElement>(root, '[data-limit]');
	const limitOut = qs(root, '[data-limit-out]');
	const expiryBtn = qs<HTMLButtonElement>(root, '[data-act="expiry"]');
	const stat = (k: string) => qs(root, `[data-stat="${k}"]`);

	function make() {
		const s = new WorkerSim({ limit });
		s.minIdlePct = ratio;
		s.expiry = expiry ? EXPIRY : 0;
		return s;
	}

	const code = () => {
		const opts: string[] = [];
		if (ratio) opts.push(`varmq.WithMinIdleWorkerRatio(${ratio}),`);
		if (expiry) opts.push('varmq.WithIdleWorkerExpiryDuration(4*time.Second),');
		const src = opts.length
			? `worker := varmq.NewWorker(process, ${limit},\n\t${opts.join('\n\t')}\n)`
			: `worker := varmq.NewWorker(process, ${limit})`;
		codeEl.innerHTML = highlightGo(src);
	};

	const render = () => {
		view.update();
		setText(stat('limit'), String(sim.numConcurrency()));
		setText(stat('idle'), String(sim.numIdleWorkers()));
		setText(stat('min'), String(sim.minIdle()));
		setText(stat('sweep'), sim.expiry ? `${(sim.sweepIn() / 1000).toFixed(1)} s` : 'no timer');
	};

	const rebuild = () => {
		sim = make();
		view.reset(sim);
		code();
		render();
	};

	// Says how many goroutines stay once the burst is over: checked once a second of model time.
	let lastIdle = -1;
	let since = 0;
	const announceIdle = () => {
		if (sim.numProcessing() === 0 && sim.numIdleWorkers() !== lastIdle) {
			lastIdle = sim.numIdleWorkers();
			live(`${lastIdle} idle ${lastIdle === 1 ? 'goroutine' : 'goroutines'} in the pool.`);
		}
	};

	const clk = clock(root, (dt) => {
		sim.advance(dt);
		render();
		since += dt;
		if (since >= 1000) {
			since = 0;
			announceIdle();
		}
	});

	limitIn.addEventListener('input', () => {
		limit = Number(limitIn.value);
		limitOut.textContent = String(limit);
		rebuild();
		say(`varmq.NewWorker(process, ${limit}) // a new worker, 1 idle goroutine`);
	});

	tabs(qs(root, '[data-ratio]'), (v) => {
		ratio = Number(v);
		rebuild();
		say(
			ratio
				? `varmq.WithMinIdleWorkerRatio(${ratio}) // keeps max(${limit} * ${ratio} / 100, 1) = ${sim.minIdle()} idle`
				: `// no ratio: the worker keeps 1 idle goroutine`,
		);
	});

	root.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLElement>('[data-act]');
		if (!btn) return;
		if (btn.dataset.act === 'expiry') {
			expiry = !expiry;
			expiryBtn.setAttribute('aria-pressed', String(expiry));
			expiryBtn.textContent = `Expiry timer: ${expiry ? '4 s' : 'off'}`;
			rebuild();
			say(expiry ? 'varmq.WithIdleWorkerExpiryDuration(4*time.Second) // a sweep every 4 s' : '// no timer: the pool shrinks as goroutines finish');
		} else if (btn.dataset.act === 'burst') {
			for (let i = 0; i < limit; i++) sim.add({ dur: DUR });
			say(`queue.Add(...) // ${limit} jobs, one for each slot`);
			if (reducedMotion()) {
				// The clock stands still: show the pool after the burst, and after the sweeps if a timer is set.
				settle((ms) => sim.advance(ms), () => sim.moving() || (sim.expiry > 0 && sim.numIdleWorkers() > sim.minIdle()));
				lastIdle = sim.numIdleWorkers();
				live(`${limit} jobs ran. ${lastIdle} idle ${lastIdle === 1 ? 'goroutine stays' : 'goroutines stay'} in the pool.`);
			} else live(`${limit} jobs added.`);
			clk.wake();
			render();
		}
	});

	code();
	render();
}
