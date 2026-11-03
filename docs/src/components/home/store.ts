// JOB 03's stage: what a restart of your process does to its jobs, for each place they can live, drawn as a piece of
// the hero map (mini.ts). The process holds Add() and the worker. In memory the queue is in the process too; with Redis
// or SQLite it is in the store below, with the jobs taken and not yet acknowledged in its IN FLIGHT tray, as on the
// map. Each event (take a job, run it, Add 5 jobs, Restart process) is a short anime.js timeline on the stage.
//
// What a restart does, as varmq, redisq and sqliteq do it (persistence/overview, redis and sqlite pages):
// - In memory, the queue and the job that was running end with the process.
// - With SQLite, the waiting rows stay. The running job's row is `processing`; the restarted program opens the queue,
//   sqliteq sets it back to `pending`, and it runs again, first.
// - With Redis, the waiting jobs stay. The running job's record is in `:nacked`; once redisq sees the crashed run is
//   gone, it goes back to the front of the queue and runs again, first, as with SQLite.
//   ponytail: shown at the restart, as the redisq presence change (plain queues seen gone when their wake-channel
//   subscription drops) does it; redisq v1.5.0 still waits for the 30 s heartbeat, so ship this with that release.

import type { Timeline } from 'animejs';
import { reducedMotion } from '../ui/dom.ts';
import { E, Job, MS, Ring, Stage, add, done, fly, linePx, pt, put, timeline, tween, type Doc, type Flight, type Lane, type Pt } from './mini.ts';

export type Mode = 'memory' | 'redis' | 'sqlite';

const NAMES: Record<Mode, string> = { memory: 'In memory', redis: 'Redis', sqlite: 'SQLite' };
const SHOW = 5; // squares a lane shows; the rest are counted
const START = 6; // jobs waiting when a tab opens
const RUN = 1300; // a job's run
const DOWN = 1000; // from the crash to the restart
const HOLD = 700; // a job put back waits at the front this long before the worker takes it, so the move reads
const REFILL = 1400; // an empty queue waits this long before the demo adds 5 more

/** The stage for a mode: its pieces, and the routes jobs take between them. */
export interface Drawn {
	stage: Stage;
	ring: Ring;
	lane: Lane;
	tray?: Lane;
	/** The process: its crash's flash and labels. */
	proc: { flash: number; name: number; died: number; back: number };
	/** How many wait past the lane's last slot. */
	more: { n: number };
	/** From just past Add() to the lane's tail, before the slot. */
	fromAdd: Pt[];
	/** From the lane's head to the ring, after the slot. */
	toRing: Pt[];
	/** From the worker's card to the tray, before the slot. */
	ack: Pt[];
}

type Box = { x: number; y: number; w: number; h: number };
/** Where the pieces sit: side by side on a wide stage, the process over the store on a narrow one. */
interface Layout {
	w: number;
	h: number;
	prog: Box;
	add: { x: number; cy: number };
	card: Box;
	ring: Pt;
	store: Box;
	/** Memory: the lane in the process, its lines, and the route into it and out to the ring. */
	mem: { lane: Pt; lines: string[]; fromAdd: Pt[]; toRing: Pt[] };
	/** Redis and SQLite: the store's lane and tray, the lines, and the routes. */
	db: { lane: Pt; tray: Pt; lines: string[]; ack: string; fromAdd: Pt[]; toRing: Pt[]; ackRoute: Pt[] };
}

const WIDE: Layout = {
	w: 520,
	h: 336,
	prog: { x: 10, y: 30, w: 500, h: 104 },
	add: { x: 30, cy: 82 },
	card: { x: 330, y: 52, w: 160, h: 60 },
	ring: pt(410, 82),
	store: { x: 150, y: 176, w: 220, h: 152 },
	mem: {
		lane: pt(126, 60),
		lines: ['M100 82H124', 'M298 82H328'],
		fromAdd: [pt(112, 82), pt(124, 82)],
		toRing: [pt(410, 82)],
	},
	db: {
		lane: pt(175, 204),
		tray: pt(175, 278),
		lines: ['M64 98V226H173', 'M347 226H410V114'],
		ack: 'M440 114V300H347',
		fromAdd: [pt(64, 111), pt(64, 226), pt(173, 226)],
		toRing: [pt(410, 226), pt(410, 114), pt(410, 82)],
		ackRoute: [pt(440, 114), pt(440, 300), pt(347, 300)],
	},
};

const NARROW: Layout = {
	w: 360,
	h: 388,
	prog: { x: 8, y: 30, w: 344, h: 154 },
	add: { x: 20, cy: 82 },
	card: { x: 200, y: 124, w: 140, h: 48 },
	ring: pt(300, 148),
	store: { x: 70, y: 228, w: 220, h: 152 },
	mem: {
		lane: pt(100, 60),
		lines: ['M90 82H98', 'M272 82H300V122'],
		fromAdd: [pt(99, 82)],
		toRing: [pt(300, 82), pt(300, 122), pt(300, 148)],
	},
	db: {
		lane: pt(95, 256),
		tray: pt(95, 330),
		lines: ['M54 98V278H93', 'M267 278H300V174'],
		ack: 'M330 174V352H267',
		fromAdd: [pt(54, 111), pt(54, 278), pt(93, 278)],
		toRing: [pt(300, 278), pt(300, 174), pt(300, 148)],
		ackRoute: [pt(330, 174), pt(330, 352), pt(267, 352)],
	},
};

/** The stage narrower than this, in CSS px, takes the narrow layout. */
export const NARROW_UNDER = 480;

export function draw(doc: Doc, mode: Mode, narrow = false): Drawn {
	const L = narrow ? NARROW : WIDE;
	const S = new Stage(doc, L.w, L.h);
	const persistent = mode !== 'memory';

	const g = add(S.world, 'g', { class: 'vm-piece vm-program' });
	const box = { x: L.prog.x, y: L.prog.y, width: L.prog.w, height: L.prog.h, rx: 16 };
	add(g, 'rect', { class: 'vm-shape', ...box });
	const flash = add(g, 'rect', { class: 'vm-flash', ...box, opacity: 0 });
	const top = box.y - 9;
	const name = S.label(g, box.x, top, 'YOUR PROCESS');
	const died = S.label(g, box.x, top, 'YOUR PROCESS · DIED', { class: 'vm-label vm-died', opacity: 0 });
	const back = S.label(g, box.x, top, 'YOUR PROCESS · RESTARTED', { opacity: 0 });
	const proc = { flash: 0, name: 1, died: 0, back: 0 };
	S.draw(() => {
		put(flash, 'opacity', proc.flash.toFixed(3));
		put(name, 'opacity', proc.name.toFixed(3));
		put(died, 'opacity', proc.died.toFixed(3));
		put(back, 'opacity', proc.back.toFixed(3));
	});

	S.chip(L.add.x, L.add.cy, 68, 'Add()');
	S.card(L.card.x, L.card.y, L.card.w, L.card.h, 'WORKER');

	const { x, y, w, h } = L.store;
	let lane: Lane;
	let tray: Lane | undefined;
	let fromAdd: Pt[];
	let toRing: Pt[];
	if (persistent) {
		S.store(x, y, w, h, mode === 'redis' ? 'REDIS' : 'SQLITE · JOBS.DB', mode === 'redis' ? 'r-store' : 's-store');
		lane = S.lane(L.db.lane.x, L.db.lane.y, SHOW, 'QUEUE');
		tray = S.lane(L.db.tray.x, L.db.tray.y, SHOW, 'IN FLIGHT', 'tray');
		for (const d of L.db.lines) S.line(d);
		S.line(L.db.ack, 'ack');
		fromAdd = L.db.fromAdd;
		toRing = [lane.end, ...L.db.toRing];
	} else {
		lane = S.lane(L.mem.lane.x, L.mem.lane.y, SHOW, 'QUEUE');
		for (const d of L.mem.lines) S.line(d);
		const none = add(S.world, 'g', { class: 'vm-piece vm-none' });
		add(none, 'rect', { class: 'vm-shape', x, y, width: w, height: h, rx: 14 });
		S.label(none, x, y - 9, 'STORAGE · NONE');
		fromAdd = L.mem.fromAdd;
		toRing = [lane.end, ...L.mem.toRing];
	}
	const count = S.label(S.world, lane.end.x, lane.end.y - 31, '', { 'text-anchor': 'end' });
	const more = { n: 0 };
	S.draw(() => {
		const text = more.n > 0 ? `+${more.n}` : '';
		if (count.textContent !== text) count.textContent = text;
	});

	const ring = new Ring(S, L.ring.x, L.ring.y, 1);
	S.layer();
	return { stage: S, ring, lane, tray, proc, more, fromAdd, toRing, ack: L.db.ackRoute };
}

/** Jobs waiting in the lane, placed at once: a tab's start, and the page's first frame (Storage.astro). */
export function place(d: Drawn, ids: number[]): Item[] {
	const items = ids.map((id, k) => {
		const rec = new Job(d.stage, d.lane.slot(Math.min(k, SHOW)), String(id));
		rec.st.o = k < SHOW ? 1 : 0;
		return { id, rec };
	});
	d.more.n = Math.max(0, ids.length - SHOW);
	return items;
}

/** The first frame: in memory, START jobs waiting. */
export function still(doc: Doc): Drawn {
	const d = draw(doc, 'memory');
	place(d, Array.from({ length: START }, (_, i) => i + 1));
	d.stage.render();
	return d;
}

/** A job: its square in the lane (in a store, its record), and while it runs, its copy on the ring. */
export interface Item {
	id: number;
	rec: Job;
	copy?: Job;
	/** Put back after a restart: the line says so when the worker takes it. */
	again?: boolean;
	/** While it flies in from Add(): its flight, which ends at its slot, and when it lands on the add's timeline. */
	inbound?: { f: Flight; end: number; t: Timeline };
}

/** The page's stage: draws it for `mode` and plays it, while it is in view. */
export function boot(root: HTMLElement) {
	const host = root.querySelector<HTMLElement>('[data-stage]')!;
	const result = root.querySelector<HTMLElement>('[data-result]')!;
	const reduce = reducedMotion();

	let mode: Mode = 'memory';
	let d: Drawn;
	let waiting: Item[] = [];
	let running: Item | null = null;
	let inflight: Item[] = [];
	let up = true;
	/** A change to the lane is under way (a take, an add, a requeue); the next waits in `queued`. */
	let laneBusy = false;
	/** The change under way is an add: the worker may still take a job beside it. */
	let adding = false;
	let queued: (() => void)[] = [];
	let refilling = false;
	let gen = 0;
	let nextId = 1;
	let visible = false;
	let narrow = false;
	const live = new Set<Timeline>();
	/** The run on the ring, and its ack on the way: a restart stops them where they are. */
	let runT: Timeline | null = null;
	let ackDot: Job | null = null;

	const persistent = () => mode !== 'memory';
	const say = (text: string) => (result.textContent = text);
	const jobs = (n: number) => `${n} ${n === 1 ? 'job' : 'jobs'}`;
	const tl = () => timeline(d.stage, false, 0);
	const newId = () => {
		const id = nextId;
		nextId = (nextId % 99) + 1;
		return id;
	};
	const slot = (k: number) => d.lane.slot(Math.min(k, SHOW));
	/** How long until a job flying in from Add() is in its slot: 0 once it is. */
	const arrival = (it: Item) => (it.inbound ? Math.max(0, it.inbound.end - it.inbound.t.currentTime) : 0);

	/**
	 * Plays a timeline (or, under reduced motion, jumps to its end), then `then`, unless a restart or a tab came first.
	 * A timeline with nothing on it (an add whose jobs all wait past the lane's last slot) ends at once: anime.js never
	 * ends one of no length.
	 */
	function play(t: Timeline, then?: () => void) {
		const mine = gen;
		const after = () => {
			live.delete(t);
			d.more.n = Math.max(0, waiting.length - SHOW);
			d.stage.render();
			if (mine === gen) then?.();
		};
		if (reduce || !t.duration) {
			t.seek(t.duration);
			after();
			return;
		}
		live.add(t);
		t.then(after);
		if (visible) t.play();
	}

	/** Runs a change to the lane now, or after the one under way. */
	function lane(change: () => void) {
		if (laneBusy) queued.push(change);
		else change();
	}
	function laneDone() {
		laneBusy = false;
		const next = queued.shift();
		if (next) next();
		else step();
	}

	/** Moves the waiting squares to their slots, from where they are. */
	function reflow(t: Timeline, at: number, from: (k: number) => number) {
		waiting.forEach((it, k) => {
			const was = from(k);
			if (was < 0 || was === k) return; // new to the lane, or where it was
			if (arrival(it)) {
				// Still flying in from Add(): its flight now ends at its new slot.
				const r = it.inbound!.f.route;
				r[r.length - 1] = slot(k);
				return;
			}
			it.rec.settle();
			fly(t, it.rec, [it.rec.home, slot(k)], at, MS.reflow);
			const shown = (n: number) => (n < SHOW ? 1 : 0);
			if (shown(was) !== shown(k)) tween(t, it.rec.st, { o: [shown(was), shown(k)] }, at, MS.reflow);
		});
	}

	/**
	 * The worker takes the job at the head of the queue and runs it. During an add it does not wait for the add: the
	 * add keeps the lane, and a job still flying in is taken once it is in its slot.
	 */
	function take() {
		const own = !laneBusy;
		laneBusy = true;
		const it = waiting.shift()!;
		running = it;
		const t = tl();
		const at = arrival(it);
		let from: Pt;
		if (at) from = slot(0);
		else {
			it.rec.settle();
			from = it.rec.home;
		}
		it.inbound = undefined;
		if (it.again) {
			it.again = false;
			say(`${result.textContent} Job ${it.id} runs again now.`);
		}
		if (persistent()) {
			// The record drops into the tray, an outline while the job is out, and a copy goes to the worker.
			const k = inflight.length;
			inflight.push(it);
			fly(t, it.rec, [from, d.tray!.slot(Math.min(k, SHOW - 1))], at, 420, E.fade);
			tween(t, it.rec.st, { f: [1, 0] }, at, 420);
			const copy = new Job(d.stage, from, String(it.id));
			it.copy = copy;
			tween(t, copy.st, { o: [0, 1] }, at, 1);
			fly(t, copy, [from, ...d.toRing], at);
			tween(t, copy.st, { s: [1, 0.5] }, at + MS.fly - 250, 250);
		} else {
			fly(t, it.rec, [from, ...d.toRing], at);
			tween(t, it.rec.st, { s: [1, 0.5] }, at + MS.fly - 250, 250);
		}
		reflow(t, at + 150, (k) => k + 1);
		play(t, () => {
			if (own) laneDone();
			runJob(it);
		});
	}

	/** The job's run on the ring, then its end: the check, and with a store, the ack that clears its record. */
	function runJob(it: Item) {
		if (reduce) {
			// No run plays out: the job stays on the ring, part way round.
			d.ring.st.p = 0.5;
			d.stage.render();
			return;
		}
		const t = tl();
		runT = t;
		tween(t, d.ring.st, { p: [0, 1] }, 0, RUN, (x) => x);
		done(t, d.ring, RUN);
		const sq = it.copy ?? it.rec;
		tween(t, sq.st, { o: [1, 0], s: [0.5, 0.2] }, RUN + 150, MS.fade, E.out);
		if (persistent()) {
			const dot = new Job(d.stage, d.ack[0], '', 'vm-ack-dot');
			ackDot = dot;
			const k = inflight.indexOf(it);
			tween(t, dot.st, { o: [0, 1], s: [0.4, 0.4] }, RUN + 150, 1);
			fly(t, dot, [...d.ack, d.tray!.slot(Math.min(k, SHOW - 1))], RUN + 150, 600, E.fade);
			tween(t, dot.st, { o: [1, 0] }, RUN + 750, 150);
			tween(t, it.rec.st, { o: [1, 0] }, RUN + 700, MS.fade);
			play(t, () => {
				runT = ackDot = null;
				dot.remove();
				it.copy?.remove();
				it.rec.remove();
				inflight.splice(inflight.indexOf(it), 1);
				running = null;
				trayReflow();
				step();
			});
		} else {
			play(t, () => {
				runT = null;
				it.rec.remove();
				running = null;
				step();
			});
		}
	}

	/** Records in the tray move up as one leaves. */
	function trayReflow() {
		if (!d.tray) return;
		const t = tl();
		inflight.forEach((it, k) => {
			it.rec.settle();
			const to = d.tray!.slot(Math.min(k, SHOW - 1));
			if (it.rec.home.x !== to.x) fly(t, it.rec, [it.rec.home, to], 0, MS.reflow);
		});
		play(t);
	}

	/** Adds n jobs from Add(): into the lane in the process, or down into the store's. */
	function addJobs(n: number, quiet = false) {
		lane(() => {
			laneBusy = adding = true;
			const t = tl();
			const added: Item[] = [];
			for (let i = 0; i < n; i++) {
				const k = waiting.length;
				const id = newId();
				const it: Item = { id, rec: new Job(d.stage, d.fromAdd[0], String(id)) };
				waiting.push(it);
				added.push(it);
				const at = i * 120;
				if (k < SHOW) {
					tween(t, it.rec.st, { o: [0, 1], s: [0.4, 1] }, at, MS.pop);
					it.inbound = { f: fly(t, it.rec, [...d.fromAdd, slot(k)], at), end: at + MS.fly, t };
				} else {
					it.rec.home = slot(k);
				}
			}
			if (!quiet) say(`${jobs(n)} added. ${waiting.length} waiting.`);
			play(t, () => {
				for (const it of added) it.inbound = undefined;
				adding = false;
				laneDone();
			});
			// The worker, if free, takes the head job now rather than after the add.
			step();
		});
	}

	/** Places n waiting jobs in the lane at once: a tab's start. */
	function fill(n: number) {
		waiting.push(...place(d, Array.from({ length: n }, newId)));
	}

	/** What happens next on its own: the worker takes the next job, or the demo adds more once the queue is empty. */
	function step() {
		if (!up || running || (laneBusy && !adding)) return;
		if (reduce) {
			if (waiting.length) take();
			return;
		}
		if (!visible) return;
		if (waiting.length) return take();
		if (laneBusy || refilling) return;
		refilling = true;
		const t = tl();
		t.add({ x: 0 }, { x: 1, duration: REFILL }, 0);
		play(t, () => {
			refilling = false;
			if (!running && !waiting.length && !laneBusy) addJobs(5, true);
		});
	}

	/**
	 * Stops everything under way and drops what was to follow. Moves end where the model already has them; the run on the
	 * ring stops where it is, since the job has not finished.
	 */
	function settleAll() {
		gen++;
		for (const t of live) {
			if (t !== runT) t.seek(t.duration);
			t.pause();
		}
		live.clear();
		runT = null;
		ackDot?.remove();
		ackDot = null;
		queued = [];
		laneBusy = adding = false;
		for (const it of waiting) it.inbound = undefined;
		refilling = false;
		d.stage.render();
	}

	function restart() {
		if (!up) return;
		settleAll();
		up = false;
		const t = tl();
		const was = running;
		running = null;
		const w = waiting.length;
		// The crash: the flash, DIED, and the process's goroutine is gone.
		tween(t, d.proc, { flash: [0, 1], name: [1, 0], back: [d.proc.back, 0] }, 0, 160);
		tween(t, d.proc, { died: [0, 1] }, 0, 160);
		tween(t, d.ring.st, { o: [1, 0], p: [d.ring.st.p, 0] }, 0, MS.fade);
		const vanish = (j: Job, at: number) => tween(t, j.st, { o: [j.st.o, 0], s: [j.st.s, j.st.s * 0.4] }, at, 400, E.vanish);
		if (was) vanish(was.copy ?? was.rec, 80);
		// A record whose ack had not landed is still in flight, whole.
		if (was && persistent()) tween(t, was.rec.st, { o: [was.rec.st.o, 1] }, 0, 160);
		let lost: Item[] = [];
		if (!persistent()) {
			lost = [...waiting, ...(was ? [was] : [])];
			waiting.forEach((it, k) => vanish(it.rec, 120 + 40 * Math.min(k, SHOW)));
			waiting = [];
			const n = lost.length;
			say(
				n === 0
					? 'Nothing was waiting. A queue in memory ends with the process.'
					: `${jobs(n)} lost${was ? (w ? `: ${w} waiting, and job ${was.id}, which was running` : `: job ${was.id}, which was running`) : ''}. A queue in memory ends with the process.`,
			);
		} else {
			const held = w ? `${jobs(w)} waiting ${w === 1 ? 'stays' : 'stay'} in ${NAMES[mode]}.` : `Nothing was waiting in ${NAMES[mode]}.`;
			const back =
				mode === 'sqlite'
					? 'sqliteq puts it back when the restarted program opens the queue, and it runs again.'
					: 'redisq puts it back once it sees the crashed run is gone, and it runs again.';
			say(was ? `${held} Job ${was.id} was running and not acknowledged: ${back}` : held);
		}
		// The restart: RESTARTED, and a new goroutine.
		tween(t, d.proc, { flash: [1, 0], died: [1, 0], back: [0, 1] }, DOWN, 300);
		tween(t, d.ring.st, { o: [0, 1], s: [0.5, 1] }, DOWN + 100, MS.pop);
		if (persistent()) requeue(t, DOWN + 200, inflight.splice(0));
		play(t, () => {
			for (const it of lost) it.rec.remove();
			if (was?.copy) {
				was.copy.remove();
				was.copy = undefined;
			}
			up = true;
			step();
		});
	}

	/** Records in flight go back to the front of the queue, filled again, and the waiting ones move back. */
	function requeue(t: Timeline, at: number, items: Item[]) {
		if (!items.length) return;
		const old = waiting;
		waiting = [...items, ...old];
		items.forEach((it, k) => {
			it.rec.settle();
			it.again = true;
			fly(t, it.rec, [it.rec.home, slot(k)], at, MS.fly, E.move);
			tween(t, it.rec.st, { o: [it.rec.st.o, k < SHOW ? 1 : 0], f: [0, 1] }, at, MS.fly);
		});
		reflow(t, at, (k) => k - items.length);
		// It waits at the front a moment, so the move back reads before the worker takes it.
		t.add({ x: 0 }, { x: 1, duration: 1 }, at + MS.fly + HOLD);
	}

	function open(next: Mode) {
		if (d) settleAll();
		mode = next;
		narrow = host.clientWidth > 0 && host.clientWidth < NARROW_UNDER;
		d = draw(document, mode, narrow);
		waiting = [];
		running = null;
		inflight = [];
		up = true;
		fill(START);
		fit();
		host.replaceChildren(d.stage.svg);
		d.stage.render();
		step();
	}

	// The light map's line floor (map.css).
	const fit = () => linePx(host, d.stage);
	// A stage that crosses NARROW_UNDER starts again in the other layout.
	new ResizeObserver(() => {
		const now = host.clientWidth > 0 && host.clientWidth < NARROW_UNDER;
		if (now !== narrow) {
			open(mode);
			say('');
		} else fit();
	}).observe(host);

	root.addEventListener('click', (e) => {
		const act = (e.target as HTMLElement).closest<HTMLElement>('[data-act]')?.dataset.act;
		if (act === 'restart') restart();
		if (act === 'add') {
			if (up) addJobs(5);
			else say('The process is restarting.');
		}
	});

	document.addEventListener('tabchange', (e) => {
		const tab = e.target as HTMLElement;
		if (!tab.closest('[data-store-tabs]')) return;
		open((tab.dataset.key as Mode) ?? 'memory');
		say('');
	});

	// The whole demo, stage and buttons: a click on a button in view plays what it starts.
	new IntersectionObserver(([entry]) => {
		visible = entry.isIntersecting;
		if (visible) {
			for (const t of live) t.play();
			step();
		} else {
			for (const t of live) t.pause();
		}
	}).observe(root);

	open('memory');
}
