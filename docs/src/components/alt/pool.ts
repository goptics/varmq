// Hero simulation: one square is one job, one lane is one worker.
// It is a drawing of the model, not a benchmark.

interface Job {
	id: number;
	dur: number;
}

interface Lane {
	el: HTMLElement;
	jobEl: HTMLElement;
	job: Job | null;
	t: number;
}

const MAX = 6;
const DOTS = 48;
const SHOW = 22;

const root = document.querySelector<HTMLElement>('[data-pool]');
if (root) init(root);

function init(root: HTMLElement) {
	const $ = <T extends HTMLElement>(s: string) => root.querySelector<T>(s)!;
	const $$ = <T extends HTMLElement>(s: string) => Array.from(root.querySelectorAll<T>(s));
	const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');

	const queueEl = $('[data-queue]');
	const dots = $$('[data-dots] i');
	const liveEl = $('[data-live]');
	const callEl = $('[data-call]');
	const tallyEl = $('[data-tally]');
	const pauseBtn = $<HTMLButtonElement>('[data-act="toggle"]');
	const prodBtn = $<HTMLButtonElement>('[data-act="producer"]');
	const stats = {
		state: $$('[data-stat="state"]'),
		workers: $$('[data-stat="workers"]'),
		waiting: $$('[data-stat="waiting"]'),
		done: $$('[data-stat="done"]'),
	};

	const lanes: Lane[] = $$('[data-lane]').map((el) => ({
		el,
		jobEl: el.querySelector<HTMLElement>('.lane__job')!,
		job: null,
		t: 0,
	}));

	const makeJob = (id: number): Job => ({ id, dur: 1500 + Math.random() * 2200 });

	let workers = 3;
	let paused = false;
	let producer = !reduce.matches;
	let nextId = 10;
	let done = 0;
	let queue: Job[] = Array.from({ length: 9 }, (_, i) => makeJob(i + 1));
	let sig = '';
	let busySig = '';
	let lit = 0;
	let evt = '';
	let visible = true;

	const stateName = () => {
		const running = lanes.some((l) => l.job);
		if (paused) return running ? 'pausing' : 'paused';
		return running || queue.length ? 'running' : 'idle';
	};

	const add = (n: number) => {
		for (let i = 0; i < n; i++) queue.push(makeJob(nextId++));
	};

	const say = (call: string, msg: string) => {
		callEl.textContent = call;
		callEl.classList.remove('is-new');
		void callEl.offsetWidth;
		callEl.classList.add('is-new');
		liveEl.textContent = msg;
	};

	const step = (dt: number, produce: boolean) => {
		for (const l of lanes) {
			if (!l.job) continue;
			l.t += dt;
			if (l.t >= l.job.dur) {
				l.job = null;
				done++;
			}
		}
		for (let i = 0; i < lanes.length; i++) {
			const l = lanes[i];
			if (!l.job && i < workers && !paused && queue.length) {
				l.job = queue.shift()!;
				l.t = 0;
			}
		}
		if (produce && producer && queue.length < 60) {
			acc += dt;
			if (acc >= 1100) {
				acc = 0;
				add(1);
			}
		}
	};

	let acc = 0;

	const render = () => {
		const state = stateName();

		const qSig = `${queue.length}:${queue[0]?.id ?? 0}`;
		if (qSig !== sig) {
			sig = qSig;
			const nodes = queue.slice(0, SHOW).map((j, i) => {
				const s = document.createElement('span');
				s.className = 'job' + (i === 0 ? ' is-next' : '');
				s.textContent = String(j.id);
				return s;
			});
			if (queue.length > SHOW) {
				const more = document.createElement('span');
				more.className = 'job job--more';
				more.textContent = `+${queue.length - SHOW}`;
				nodes.push(more);
			}
			queueEl.replaceChildren(...nodes);
		}

		lanes.forEach((l, i) => {
			const busy = !!l.job;
			l.el.toggleAttribute('data-busy', busy);
			l.el.toggleAttribute('data-off', !busy && i >= workers);
			if (busy && l.job) {
				l.el.style.setProperty('--p', Math.min(1, l.t / l.job.dur).toFixed(3));
				if (l.jobEl.dataset.id !== String(l.job.id)) {
					l.jobEl.dataset.id = String(l.job.id);
					l.jobEl.textContent = String(l.job.id);
					l.jobEl.style.animation = 'none';
					void l.jobEl.offsetWidth;
					l.jobEl.style.animation = '';
				}
			} else {
				l.el.style.setProperty('--p', '0');
				delete l.jobEl.dataset.id;
			}
		});

		const nextLit = Math.min(done, DOTS);
		if (nextLit !== lit) {
			dots.forEach((d, i) => d.classList.toggle('on', i < nextLit));
			lit = nextLit;
			tallyEl.textContent = `${done} done`;
		}

		const bSig = `${state}|${workers}|${queue.length}|${done}`;
		if (bSig !== busySig) {
			busySig = bSig;
			for (const el of stats.state) el.textContent = state;
			for (const el of stats.workers) el.textContent = String(workers);
			for (const el of stats.waiting) el.textContent = String(queue.length);
			for (const el of stats.done) el.textContent = String(done);
			pauseBtn.textContent = paused ? 'Resume' : 'Pause';
			pauseBtn.disabled = state === 'pausing';
			const e = JSON.stringify({ workers, waiting: queue.length, state, done });
			if (e !== evt) {
				evt = e;
				document.dispatchEvent(new CustomEvent('varmq:pool', { detail: { workers, waiting: queue.length, state, done } }));
			}
		}
	};

	let last = performance.now();
	let since = 0;
	const frame = (now: number) => {
		const dt = Math.min(now - last, 100);
		last = now;
		if (visible) {
			if (reduce.matches) {
				since += dt;
				if (since >= 250) {
					step(since, true);
					since = 0;
					render();
				}
			} else {
				step(dt, true);
				render();
			}
		}
		requestAnimationFrame(frame);
	};

	new IntersectionObserver(([entry]) => {
		visible = entry.isIntersecting;
		last = performance.now();
	}).observe(root);

	root.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-act]');
		if (!btn) return;
		switch (btn.dataset.act) {
			case 'add':
				add(1);
				say('queue.Add(job)', `Job ${nextId - 1} added. ${queue.length} waiting.`);
				break;
			case 'burst':
				add(10);
				say('for range 10 { queue.Add(job) }', `10 jobs added. ${queue.length} waiting.`);
				break;
			case 'more':
				if (workers < MAX) {
					workers++;
					say(`worker.TunePool(${workers})`, `The pool now has ${workers} workers.`);
				}
				break;
			case 'fewer':
				if (workers > 1) {
					workers--;
					say(`worker.TunePool(${workers})`, `The pool now has ${workers} workers.`);
				}
				break;
			case 'toggle':
				paused = !paused;
				say(paused ? 'worker.Pause()' : 'worker.Resume()', paused ? 'Worker paused. Running jobs finish.' : 'Worker resumed.');
				break;
			case 'producer':
				producer = !producer;
				prodBtn.textContent = producer ? 'Producer: on' : 'Producer: off';
				prodBtn.setAttribute('aria-pressed', String(producer));
				liveEl.textContent = producer ? 'The simulated producer is on.' : 'The simulated producer is off.';
				break;
		}
		if (reduce.matches) {
			step(0, false);
		}
		render();
	});

	prodBtn.textContent = producer ? 'Producer: on' : 'Producer: off';
	prodBtn.setAttribute('aria-pressed', String(producer));
	step(0, false);
	render();
	requestAnimationFrame(frame);
}
