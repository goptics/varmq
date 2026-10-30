// Concurrency scene: a slider that calls TunePool on a running worker, the goroutines it makes,
// and the throughput the model gives. Each job takes 2.4 s. A producer adds a job every 0.7 s.

import { WorkerSim } from './sim';
import { PoolView } from './poolview';
import { reducedMotion } from '../ui/dom';
import { announcer, caller, clock, qs, qsa, setText } from './scene';

const DUR = 2400;
const ARRIVE = 700;
const WINDOW = 8000;
const SCALE = 5.5; // jobs per second at the right end of the meter

qsa<HTMLElement>(document, '[data-scene="gb-concurrency"]').forEach(init);

function init(root: HTMLElement) {
	const sim = new WorkerSim({ limit: 3 });
	const view = new PoolView(qs(root, '[data-pool]'), sim);
	const say = caller(root);
	const live = announcer(root);

	const limitIn = qs<HTMLInputElement>(root, '[data-limit]');
	const limitOut = qs(root, '[data-limit-out]');
	const producerBtn = qs<HTMLButtonElement>(root, '[data-act="producer"]');
	const fill = qs(root, '[data-fill]');
	const tickCap = qs(root, '[data-tick-cap]');
	const tickArr = qs(root, '[data-tick-arr]');
	const meterText = qs(root, '[data-meter-text]');
	const stat = (k: string) => qs(root, `[data-stat="${k}"]`);

	let producer = !reducedMotion();
	let acc = 0;
	const ends: number[] = [];

	sim.onEvent = (e) => {
		view.record(e);
		if (e.type === 'end') ends.push(sim.now);
	};

	const add = (n: number) => {
		for (let i = 0; i < n; i++) sim.add({ dur: DUR });
	};

	const render = () => {
		view.update();
		setText(stat('limit'), String(sim.numConcurrency()));
		setText(stat('processing'), String(sim.numProcessing()));
		setText(stat('pending'), String(sim.numPending()));
		setText(stat('idle'), String(sim.numIdleWorkers()));
		const over = sim.numProcessing() > sim.numConcurrency();
		stat('processing').dataset.warn = String(over);

		while (ends.length && ends[0] < sim.now - WINDOW) ends.shift();
		const win = Math.min(WINDOW, sim.now);
		const measured = win >= 2000 ? ends.length / (win / 1000) : 0;
		const ceiling = sim.limit / (DUR / 1000);
		const arriving = producer ? 1000 / ARRIVE : 0;
		fill.style.setProperty('--f', String(Math.min(1, measured / SCALE).toFixed(3)));
		tickCap.style.setProperty('--v', `${Math.min(100, (ceiling / SCALE) * 100)}%`);
		tickArr.style.setProperty('--v', `${Math.min(100, (arriving / SCALE) * 100)}%`);
		meterText.innerHTML =
			`done <b>${win >= 2000 ? measured.toFixed(1) : '…'}/s</b> · ceiling, limit ÷ 2.4 s: <b>${ceiling.toFixed(1)}/s</b> · ` +
			(producer ? `arriving: <b>${arriving.toFixed(1)}/s</b>` : 'producer off');
	};

	const tick = (dt: number) => {
		sim.advance(dt);
		if (producer && sim.queue.length < 60) {
			acc += dt;
			while (acc >= ARRIVE) {
				acc -= ARRIVE;
				sim.add({ dur: DUR });
			}
		}
		render();
	};

	const clk = clock(root, tick);

	const tune = (n: number) => {
		const held = sim.gors.reduce((a, g) => a + (g.busy ? Math.max(0, g.batch.length - g.i - 1) : 0), 0);
		const before = sim.limit;
		const err = sim.tunePool(n);
		let msg = `worker.TunePool(${n}) // ${err ?? 'nil'}`;
		if (!err && n > before && held > 0) msg += `, but ${held} held ${held === 1 ? 'job stays' : 'jobs stay'} in one goroutine's batch`;
		if (!err && n < before && sim.numProcessing() > n) msg += `, NumProcessing stays ${sim.numProcessing()} until goroutines finish`;
		say(msg);
		live(err ? `TunePool returned ${err}` : `Limit is ${n}. ${sim.numProcessing()} running, ${sim.numPending()} pending.`);
		render();
	};

	limitIn.addEventListener('input', () => {
		const n = Number(limitIn.value);
		limitOut.textContent = String(n);
		tune(n);
		clk.wake();
	});

	root.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLElement>('[data-act]');
		if (!btn) return;
		const act = btn.dataset.act;
		if (act === 'add3' || act === 'add12') {
			const n = act === 'add3' ? 3 : 12;
			add(n);
			say(`queue.Add(...) // ${n} times, each returns true`);
			live(`${n} jobs added. ${sim.numPending()} pending.`);
		} else if (act === 'same') {
			const err = sim.tunePool(sim.limit);
			say(`worker.TunePool(${sim.limit}) // ${err}`);
			live(`TunePool returned ${err}`);
		} else if (act === 'producer') {
			producer = !producer;
			producerBtn.setAttribute('aria-pressed', String(producer));
			producerBtn.textContent = `Producer: ${producer ? 'on' : 'off'}`;
		}
		render();
		clk.wake();
	});

	producerBtn.setAttribute('aria-pressed', String(producer));
	producerBtn.textContent = `Producer: ${producer ? 'on' : 'off'}`;

	// Start with a short backlog so the goroutines are busy.
	add(8);
	sim.advance(80);
	render();
}
