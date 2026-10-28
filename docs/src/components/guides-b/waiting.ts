// Waiting scene: the wait calls of worker_wait.go on one timeline. Each case starts workers with
// three jobs and arms calls that block until the model says they return.

import { WorkerSim, type Status } from './sim';
import { highlightGo } from './go';
import { announcer, caller, clock, esc, qs, radios, reducedMotion, sec } from './scene';

type Kind =
	| 'Wait'
	| 'WaitUntilIdle'
	| 'WaitUntilPaused'
	| 'WaitUntilStopped'
	| 'StopAndWait'
	| 'WaitAndStop'
	| 'PauseAndWait'
	| 'WaitAndPause';

interface Call {
	kind: Kind;
	at: number;
	armed: boolean;
	t0: number;
	t1?: number;
	mark?: number;
	phase: 1 | 2;
	err?: string | null;
	result?: string;
}

interface Track {
	name: string;
	sim: WorkerSim;
	status: Array<{ s: Status; from: number; to?: number }>;
	jobs: Array<{ id: number; from: number; to?: number }>;
	calls: Call[];
	pauseAt?: number;
	paused?: boolean;
}

interface Case {
	autoRun: boolean;
	tracks: Array<{ name: string; calls: Array<[Kind, number]>; pauseAt?: number }>;
	button: 'resume' | 'start' | null;
	code: string;
	hint: string;
}

const DUR = 2400;
const STEP = 40;
const MIN_SPAN = 16000;
const MAX_SPAN = 60000;

const CASES: Record<string, Case> = {
	pause: {
		autoRun: true,
		tracks: [{ name: 'worker', pauseAt: 300, calls: [['Wait', 500], ['WaitUntilIdle', 500], ['WaitUntilPaused', 500]] }],
		button: 'resume',
		code: `worker.Pause()            // job 1 runs, jobs 2 and 3 wait
worker.Wait()             // no job running
worker.WaitUntilIdle()    // Idle, nothing pending
worker.WaitUntilPaused()  // Paused`,
		hint: '// Wait and WaitUntilPaused return when job 1 ends. WaitUntilIdle waits for Resume.',
	},
	stop: {
		autoRun: true,
		tracks: [
			{ name: 'A: StopAndWait()', calls: [['StopAndWait', 400]] },
			{ name: 'B: WaitAndStop()', calls: [['WaitAndStop', 400]] },
		],
		button: null,
		code: `worker.StopAndWait()  // Stop() now, then wait for Stopped
worker.WaitAndStop()  // Wait() for the drain, then Stop()`,
		hint: '// StopAndWait leaves jobs 2 and 3 pending. WaitAndStop runs all three first.',
	},
	pauseWait: {
		autoRun: true,
		tracks: [
			{ name: 'A: PauseAndWait()', calls: [['PauseAndWait', 400]] },
			{ name: 'B: WaitAndPause()', calls: [['WaitAndPause', 400]] },
		],
		button: null,
		code: `worker.PauseAndWait()  // Pause() now, then wait for Paused
worker.WaitAndPause()  // Wait() for the drain, then Pause()`,
		hint: '// PauseAndWait returns at Paused with 2 pending. WaitAndPause lets all three run.',
	},
	initiated: {
		autoRun: false,
		tracks: [{ name: 'worker', calls: [['Wait', 400], ['WaitUntilIdle', 400]] }],
		button: 'start',
		code: `worker := varmq.NewWorker(process, varmq.WithAutoRun(false))
queue := worker.BindQueue()
worker.Wait()          // Initiated: returns at once
worker.WaitUntilIdle() // waits for Start and a drain`,
		hint: '// Wait returns at once with three jobs queued. WaitUntilIdle waits for Start().',
	},
};

document.querySelectorAll<HTMLElement>('[data-scene="waiting"]').forEach(init);

function init(root: HTMLElement) {
	const tlEl = qs(root, '[data-timeline]');
	const logEl = qs(root, '[data-log]');
	const statsEl = qs(root, '.gb-stats');
	const codeEl = qs(root, '.gb-gocode code');
	const callEl = qs(root, '[data-call]');
	const say = caller(root);
	const live = announcer(root);
	const resumeBtn = qs<HTMLButtonElement>(root, '[data-act="resume"]');
	const startBtn = qs<HTMLButtonElement>(root, '[data-act="start"]');

	let key = 'pause';
	let tracks: Track[] = [];
	let sig = '';
	let reduce = reducedMotion();
	let trackPx = 300;

	const now = () => tracks[0]?.sim.now ?? 0;

	const build = () => {
		const c = CASES[key];
		tracks = c.tracks.map((t) => {
			const sim = new WorkerSim({ limit: 1, autoRun: c.autoRun });
			const track: Track = {
				name: t.name,
				sim,
				status: [{ s: sim.status, from: 0 }],
				jobs: [],
				calls: t.calls.map(([kind, at]) => ({ kind, at, armed: false, t0: at, phase: 1 })),
				pauseAt: t.pauseAt,
			};
			sim.onEvent = (e) => {
				if (e.type === 'status') {
					track.status[track.status.length - 1].to = sim.now;
					track.status.push({ s: e.to, from: sim.now });
				} else if (e.type === 'start') track.jobs.push({ id: e.job.id, from: sim.now });
				else if (e.type === 'end') {
					const j = track.jobs.find((x) => x.id === e.job.id && x.to === undefined);
					if (j) j.to = sim.now;
				}
			};
			for (let i = 0; i < 3; i++) sim.add({ dur: DUR });
			return track;
		});
		logEl.replaceChildren();
		codeEl.innerHTML = highlightGo(c.code);
		callEl.textContent = c.hint;
		resumeBtn.hidden = c.button !== 'resume';
		startBtn.hidden = c.button !== 'start';
		sig = '';
		statsEls();
		render();
	};

	const statsEls = () => {
		const multi = tracks.length > 1;
		statsEl.innerHTML = tracks
			.map((t, i) => {
				const p = multi ? `${'AB'[i]} ` : '';
				return `<div><dt>${p}status</dt><dd data-s="${i}-status"></dd></div><div><dt>${p}NumPending</dt><dd data-s="${i}-pending"></dd></div>`;
			})
			.join('');
	};

	const log = (t: Track, c: Call) => {
		const who = tracks.length > 1 ? `${t.name.split(':')[0]} · ` : '';
		const at = c.t1 ?? 0;
		const detail = `status ${t.sim.status}, NumPending ${t.sim.numPending()}, NumProcessing ${t.sim.numProcessing()}`;
		const li = document.createElement('li');
		if (c.kind === 'WaitUntilIdle' || c.kind === 'WaitAndStop' || c.kind === 'WaitAndPause') li.dataset.tone = '';
		li.innerHTML = `<b>${esc(who)}${c.kind}()</b> returned at ${sec(at)}: ${esc(detail)}.`;
		logEl.append(li);
		live(`${who}${c.kind} returned at ${sec(at)}. NumPending ${t.sim.numPending()}.`);
		say(`${c.kind}() // returned at ${sec(at)}, NumPending ${t.sim.numPending()}`);
	};

	const arm = (t: Track, c: Call) => {
		c.armed = true;
		c.t0 = t.sim.now;
		if (c.kind === 'StopAndWait') {
			c.err = t.sim.stop();
			c.mark = t.sim.now;
		} else if (c.kind === 'PauseAndWait') {
			c.err = t.sim.pause();
			c.mark = t.sim.now;
		}
		c.phase = 1;
	};

	const done = (t: Track, c: Call) => {
		const s = t.sim;
		switch (c.kind) {
			case 'Wait':
				return s.waitDone();
			case 'WaitUntilIdle':
				return s.waitUntilIdleDone();
			case 'WaitUntilPaused':
				return s.waitUntilPausedDone();
			case 'WaitUntilStopped':
				return s.waitUntilStoppedDone();
			case 'StopAndWait':
				return !!c.err || s.waitUntilStoppedDone();
			case 'PauseAndWait':
				return !!c.err || s.waitUntilPausedDone();
			case 'WaitAndStop':
			case 'WaitAndPause':
				if (!s.waitDone()) return false;
				c.mark = s.now;
				if (c.kind === 'WaitAndStop') c.err = s.stop();
				else c.err = s.pause();
				return true;
		}
	};

	const stepOnce = (dt: number) => {
		for (const t of tracks) {
			const s = t.sim;
			s.advance(dt);
			if (t.pauseAt !== undefined && !t.paused && s.now >= t.pauseAt) {
				t.paused = true;
				s.pause();
				say('worker.Pause() // nil: Pausing while job 1 runs');
			}
			for (const c of t.calls) {
				if (!c.armed && s.now >= c.at) arm(t, c);
				if (c.armed && c.t1 === undefined && done(t, c)) {
					c.t1 = s.now;
					log(t, c);
				}
			}
		}
	};

	const canProgress = (t: Track) =>
		t.sim.busy() > 0 || (t.sim.isActive() && t.sim.queue.length > 0) || t.sim.status === 'Pausing' || t.sim.status === 'Stopping';

	/** Run the model forward until nothing moves, for reduced motion and after a press with the clock paused. */
	const settle = () => {
		for (let i = 0; i < 1200; i++) {
			stepOnce(STEP);
			const open = tracks.some((t) => t.calls.some((c) => !c.armed));
			if (!open && !tracks.some(canProgress)) break;
		}
	};

	const render = () => {
		const n = now();
		// The time scale zooms out as the run gets longer, so the first calls stay on screen.
		const SPAN = Math.min(MAX_SPAN, Math.max(MIN_SPAN, Math.ceil((n + 2000) / 4000) * 4000));
		const t0 = Math.max(0, n - (SPAN - 2000));
		const x = (ms: number) => `${(((ms - t0) / SPAN) * 100).toFixed(2)}%`;
		const w = (a: number, b: number) => `${(((Math.max(b, t0) - Math.max(a, t0)) / SPAN) * 100).toFixed(2)}%`;
		// Show a label only when the segment is wide enough to hold it.
		const trackW = trackPx;
		const fits = (a: number, b: number, text: string) => ((b - Math.max(a, t0)) / SPAN) * trackW >= text.length * 6.8 + 14;
		const seg = (cls: string, a: number, b: number, text: string, attrs = '') => {
			if (b <= t0) return '';
			const label = fits(a, b, text) ? esc(text) : '';
			return `<span class="gb-tl__seg ${cls}" style="left:${x(Math.max(a, t0))};width:${w(a, b)}" title="${esc(text)}" ${attrs}>${label}</span>`;
		};

		let axis = '';
		const tickEvery = SPAN > 30000 ? 10000 : SPAN > 20000 ? 4000 : 2000;
		for (let s = Math.ceil(t0 / tickEvery) * tickEvery; s < t0 + SPAN - 600; s += tickEvery) axis += `<span style="left:${x(s)}">${s / 1000} s</span>`;
		const nowLine = `<i class="gb-tl__now" style="left:${x(n)}"></i>`;

		let html = `<div class="gb-tl__axis" aria-hidden="true">${axis}</div>`;
		for (const t of tracks) {
			const multi = tracks.length > 1;
			html += `<div class="gb-tl__group">${multi ? `<p class="gb-tl__name">${esc(t.name)}</p>` : ''}`;
			html += `<div class="gb-tl__row"><span class="gb-tl__label">worker status</span><div class="gb-tl__track">${t.status
				.map((s) => seg('', s.from, s.to ?? n, s.s, `data-status="${s.s}"`))
				.join('')}${nowLine}</div></div>`;
			html += `<div class="gb-tl__row"><span class="gb-tl__label">job running</span><div class="gb-tl__track">${t.jobs
				.map((j) => seg('is-job', j.from, j.to ?? n, String(j.id)))
				.join('')}${nowLine}</div></div>`;
			for (const c of t.calls) {
				const a = c.armed ? c.t0 : null;
				html += `<div class="gb-tl__row"><span class="gb-tl__label">${c.kind}()</span><div class="gb-tl__track">`;
				if (a !== null) {
					const end = c.t1 ?? n;
					const text = c.t1 !== undefined ? `returns ${sec(c.t1)}` : 'blocked';
					html += seg('is-blocked', a, end, text, `data-done="${c.t1 !== undefined}"`);
					// A bar too short for its label gets the label on its right.
					if (end > t0 && !fits(a, end, text)) html += `<span class="gb-tl__out" style="left:calc(${x(end)} + 6px)">${esc(text)}</span>`;
					if (c.mark !== undefined) html += `<i class="gb-tl__mark" style="left:${x(c.mark)}" title="${c.kind.includes('Stop') ? 'Stop' : 'Pause'}() called"></i>`;
				}
				html += `${nowLine}</div></div>`;
			}
			html += '</div>';
		}
		const nsig = `${Math.round(n / 80)}:${tracks.map((t) => t.calls.map((c) => (c.t1 ?? '') + String(c.armed)).join()).join('|')}`;
		if (nsig !== sig) {
			sig = nsig;
			tlEl.innerHTML = html;
			trackPx = tlEl.querySelector<HTMLElement>('.gb-tl__track')?.clientWidth || trackPx;
		}

		tracks.forEach((t, i) => {
			const a = statsEl.querySelector(`[data-s="${i}-status"]`);
			const b = statsEl.querySelector(`[data-s="${i}-pending"]`);
			if (a) a.textContent = t.sim.status;
			if (b) b.textContent = String(t.sim.numPending());
		});
		const t = tracks[0];
		resumeBtn.disabled = !(t.sim.status === 'Paused' || t.sim.status === 'Pausing');
		startBtn.disabled = t.sim.status !== 'Initiated';
	};

	const clk = clock(root, (dt) => {
		for (let left = dt; left > 0; left -= STEP) stepOnce(Math.min(STEP, left));
		render();
	});

	const load = (k: string) => {
		key = k;
		build();
		if (reduce || clk.paused) settle();
		render();
	};

	radios(qs(root, '[data-case]'), (v) => load(v));

	root.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLElement>('[data-act]');
		if (!btn) return;
		const t = tracks[0];
		if (btn.dataset.act === 'resume') {
			const err = t.sim.resume();
			say(`worker.Resume() // ${err ?? 'nil'}: jobs 2 and 3 run, WaitUntilIdle can return`);
		} else if (btn.dataset.act === 'start') {
			const err = t.sim.start();
			say(`worker.Start() // ${err ?? 'nil'}: the three jobs run`);
		} else if (btn.dataset.act === 'replay') {
			load(key);
			say(CASES[key].hint);
		}
		if (clk.paused) settle();
		render();
		clk.wake();
	});

	reduce = reducedMotion();
	load(key);
}
