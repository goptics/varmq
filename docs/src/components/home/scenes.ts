// JOB 02's three scenes, each a small piece of the hero map with its own looping anime.js timeline (mini.ts): a
// priority queue, three queues on one worker under RoundRobin, and a topic with two subscribed workers. The order the
// jobs run in comes from order.ts, which does what varmq does, and the Go call of each step is the caption above the
// scene. Scenes.astro draws each one's still frame at build time with `build`; `boot` plays them on the page.

import type { Timeline } from 'animejs';
import { reducedMotion } from '../ui/dom.ts';
import { E, Job, MS, Ring, Stage, captionAt, done, fly, linePx, pt, put, timeline, tween, type Captions, type Doc, type Pt } from './mini.ts';
import { PRIORITY_ADDS, RR_QUEUES, priorityQueue, roundRobin } from './order.ts';

export type SceneKey = 'priority' | 'strategy' | 'topic';

const W = 360;
const H = 226;
/** The end held before the rings fade and the scene runs again. */
const HOLD = 1800;
const OUT = 450;
const AGAIN = 400;

export interface Scene {
	stage: Stage;
	tl: Timeline;
	caps: Captions;
	/** The still frame under reduced motion: a moment that shows the order, and the caption that says it. */
	still: { at: number; cap: string };
}

/** A scene's stage and its timeline; `loop` for the page, not for a still frame. */
export function build(key: SceneKey, doc: Doc, loop: boolean, onCaption?: (text: string) => void): Scene {
	const stage = new Stage(doc, W, H);
	const caps: Captions = [];
	const tl = timeline(stage, loop, AGAIN, (t) => onCaption?.(captionAt(caps, t)));
	const still = { priority, strategy, topic }[key](stage, tl, caps);
	// A tween over the whole run, so the timeline renders, and the caption follows, while nothing else moves.
	tl.add({ v: 0 }, { v: [0, 1], duration: tl.iterationDuration, ease: 'linear' }, 0);
	return { stage, tl, caps, still };
}

/** Fades the rings out after the hold, so the run starts again from nothing. */
function fadeOut(tl: Timeline, at: number, rings: Ring[]) {
	for (const g of rings) tween(tl, g.st, { o: [1, 0] }, at, OUT);
	tl.add({ end: 0 }, { end: 1, duration: 1 }, at + OUT); // the run's end
}

/** A job popping in at `at`: from 0.4 of its size, fading in. */
function popIn(tl: Timeline, job: Job, at: number) {
	tween(tl, job.st, { o: [0, 1] }, at, 200);
	tween(tl, job.st, { s: [0.4, 1] }, at, MS.pop);
}

/**
 * A job's run on a ring: it shrinks to fit, the arc goes round, the check; then the job leaves the scene at the
 * worker, as a finished job does on the hero map. Returns when it is done.
 */
function run(tl: Timeline, job: Job, ring: Ring, route: Pt[], at: number, ms: number): number {
	fly(tl, job, route, at);
	tween(tl, job.st, { s: [1, 0.5] }, at + MS.fly - 250, 250);
	const start = at + MS.fly;
	tween(tl, ring.st, { p: [0, 1] }, start, ms, (t) => t);
	done(tl, ring, start + ms);
	tween(tl, job.st, { o: [1, 0], s: [0.5, 0.2] }, start + ms + 120, MS.fade, E.vanish);
	return start + ms;
}

// ---------------------------------------------------------------------------------------------------------------
// Priority: WithAutoRun(false), three Adds, then Start. alert (1) runs first, then report and digest, the order they
// were added in.

function priority(S: Stage, tl: Timeline, caps: Captions): Scene['still'] {
	const Y = 112; // the row's middle
	const add = pt(84, Y); // where a job comes in: right of Add(), so it does not cover the call
	S.chip(10, Y, 62, 'Add()');
	S.line(`M74 ${Y}H84`);
	// Each job shows its priority alone: the number is what the queue orders by.
	const lane = S.lane(86, Y - 22, 4, 'PRIORITY QUEUE');
	S.line(`M${lane.end.x + 2} ${Y}H272`);
	S.card(274, Y - 30, 78, 60, 'WORKER');
	const ring = new Ring(S, 313, Y, 0);
	S.layer();

	const jobs = new Map(PRIORITY_ADDS.map((a) => [a.name, new Job(S, add, String(a.priority))]));
	const slotOf = new Map<string, number>();
	let t = 0;
	caps.push([0, 'varmq.NewWorker(fn, varmq.WithAutoRun(false))']);
	t += 900;

	const states = priorityQueue(PRIORITY_ADDS);
	PRIORITY_ADDS.forEach((a, i) => {
		caps.push([t, `queue.Add("${a.name}", ${a.priority})`]);
		states[i].forEach((b, k) => {
			const job = jobs.get(b.name)!;
			if (b === a) {
				popIn(tl, job, t);
				fly(tl, job, [add, lane.slot(k)], t);
			} else if (slotOf.get(b.name) !== k) {
				// Behind the new job's number now: it moves back a slot.
				fly(tl, job, [lane.slot(slotOf.get(b.name)!), lane.slot(k)], t + 120, MS.reflow);
			}
			slotOf.set(b.name, k);
		});
		t += 1000;
	});

	caps.push([t, 'worker.Start()']);
	tween(tl, ring.st, { o: [0, 1], s: [0.5, 1] }, t, MS.pop);
	t += 650;

	const order = states[states.length - 1];
	const why = ['// alert has the lowest number', '// report and digest tie: the first added runs first', '// digest runs last'];
	let still = 0;
	order.forEach((a, k) => {
		caps.push([t, why[k]]);
		const end = run(tl, jobs.get(a.name)!, ring, [lane.slot(0), lane.end, ring.at], t, 900);
		order.slice(k + 1).forEach((b, j) => fly(tl, jobs.get(b.name)!, [lane.slot(j + 1), lane.slot(j)], t + 200, MS.reflow));
		if (k === 0) still = t + MS.fly + 450; // alert on the ring, report then digest waiting
		t = end + 120 + MS.fade + 150;
	});
	caps.push([t, `// ran: ${order.map((a) => a.name).join(', ')}`]);
	fadeOut(tl, t + HOLD, [ring]);
	return { at: still, cap: `// runs: ${order.map((a) => a.name).join(', ')}` };
}

// ---------------------------------------------------------------------------------------------------------------
// Several queues, one worker: RoundRobin over alerts, reports and emails, as order.ts's roundRobin takes them.

function strategy(S: Stage, tl: Timeline, caps: Captions): Scene['still'] {
	const ys = [46, 114, 182];
	const lanes = RR_QUEUES.map((q, i) => S.lane(10, ys[i] - 22, 3, q.name.toUpperCase()));
	const loop = pt(172, 114);
	// Each lane's line to the event loop, ending at its circle.
	const leads: Pt[][] = [
		[pt(118, 46), pt(140, 46), pt(160, 101)],
		[pt(118, 114), pt(152, 114)],
		[pt(118, 182), pt(140, 182), pt(160, 127)],
	];
	for (const l of leads) S.line(`M${l.map((p) => `${p.x} ${p.y}`).join('L')}`);
	S.loop(loop.x, loop.y, 18);
	S.line('M192 114H250');
	S.chip(186, 166, 92, 'RoundRobin');
	S.card(252, 84, 100, 60, 'WORKER');
	const ring = new Ring(S, 302, 114, 0);
	S.layer();

	const jobs = new Map<string, Job>();
	let t = 0;
	caps.push([0, 'varmq.WithStrategy(varmq.RoundRobin)']);
	t += 700;
	caps.push([t, '// 2 alerts, 1 report and 3 emails added']);
	RR_QUEUES.forEach((q, i) =>
		q.jobs.forEach((id, k) => {
			const job = new Job(S, lanes[i].slot(k), id);
			jobs.set(id, job);
			popIn(tl, job, t + 90 * jobs.size);
		}),
	);
	t += 1100;
	caps.push([t, 'worker.Start()']);
	tween(tl, ring.st, { o: [0, 1], s: [0.5, 1] }, t, MS.pop);
	t += 650;

	const left = RR_QUEUES.map((q) => [...q.jobs]);
	const picks = roundRobin(RR_QUEUES.map((q) => q.jobs));
	let still = 0;
	picks.forEach((p, k) => {
		const name = RR_QUEUES[p.queue].name;
		const skipped = p.skipped.map((q) => RR_QUEUES[q].name);
		caps.push([
			t,
			`${
				skipped.length
					? `// ${skipped.join(' and ')} ${skipped.length > 1 ? 'are' : 'is'} empty, so ${name}`
					: k < RR_QUEUES.length
						? `// turn ${k + 1}: ${name}`
						: `// back to ${name}`
			}: ${p.job}`,
		]);
		const lane = lanes[p.queue];
		const end = run(tl, jobs.get(p.job)!, ring, [lane.slot(0), ...leads[p.queue], loop, pt(250, 114), ring.at], t, 600);
		left[p.queue].shift();
		left[p.queue].forEach((id, j) => fly(tl, jobs.get(id)!, [lane.slot(j + 1), lane.slot(j)], t + 200, MS.reflow));
		if (k === 0) still = t + MS.fly + 300; // a1 on the ring, the rest waiting in their queues
		t = end + 120 + MS.fade + 100;
	});
	caps.push([t, `// ran: ${picks.map((p) => p.job).join(', ')}`]);
	fadeOut(tl, t + HOLD, [ring]);
	return { at: still, cap: `// runs: ${picks.map((p) => p.job).join(', ')}` };
}

// ---------------------------------------------------------------------------------------------------------------
// Topics: two workers subscribe, one Publish, and each worker gets its own job. The topic keeps the value once, until
// both have read it.

function topic(S: Stage, tl: Timeline, caps: Captions): Scene['still'] {
	const pub = pt(52, 113);
	S.chip(10, 113, 84, 'Publish()');
	S.line('M96 113H104');
	const lane = S.lane(106, 91, 3, 'TOPIC');
	const subs = [
		{ line: S.line('M214 113H230V50H262'), y: 50, label: 'EMAIL', ms: 900 },
		{ line: S.line('M230 113V176H262'), y: 176, label: 'STOCK', ms: 400 },
	].map((s) => {
		S.card(264, s.y - 30, 88, 60, s.label);
		const ring = new Ring(S, 308, s.y, 1);
		const on = { o: 0.25 };
		S.draw(() => put(s.line, 'opacity', Number(on.o.toFixed(3))));
		return { ...s, ring, on };
	});
	S.layer();

	const value = new Job(S, pub, '7');
	const copies = subs.map(() => new Job(S, lane.slot(0), '7'));
	let t = 0;
	caps.push([0, 'topic := varmq.NewTopic[int]()']);
	t += 800;
	subs.forEach((s, i) => {
		caps.push([t, `${s.label.toLowerCase()}.WithTopic(topic)`]);
		tween(tl, s.on, { o: [0.25, 1] }, t, 400);
		t += i ? 900 : 800;
	});
	caps.push([t, 'topic.Publish(7)']);
	popIn(tl, value, t);
	fly(tl, value, [pub, lane.slot(0)], t);
	t += 1100;

	caps.push([t, '// each worker gets its own job']);
	let end = t;
	subs.forEach((s, i) => {
		const job = copies[i];
		tween(tl, job.st, { o: [0, 1] }, t, 1);
		end = Math.max(end, run(tl, job, s.ring, [lane.slot(0), lane.end, pt(230, 113), pt(230, s.y), s.ring.at], t + i * 120, s.ms));
	});
	// Both have read it: the topic frees it.
	tween(tl, value.st, { o: [1, 0] }, t + 400, MS.fade);
	const still = t + 120 + MS.fly + 200; // both copies on their rings
	t = end + 120 + MS.fade + 300;
	caps.push([t, '// email and stock both ran order 7']);
	fadeOut(tl, t + HOLD, []);
	for (const s of subs) tween(tl, s.on, { o: [1, 0.25] }, t + HOLD, OUT);
	return { at: still, cap: '// email and stock each run order 7' };
}

// ---------------------------------------------------------------------------------------------------------------

/** Plays each scene on the page: its own loop while it is in view, resting out of view; a still frame under reduced motion. */
export function boot() {
	const reduce = reducedMotion();
	for (const root of document.querySelectorAll<HTMLElement>('[data-scene]')) {
		const cap = root.closest('.scene-card')?.querySelector<HTMLElement>('[data-cap]');
		const scene = build(root.dataset.scene as SceneKey, document, !reduce, (text) => {
			if (cap && cap.textContent !== text) cap.textContent = text;
		});
		const fit = () => linePx(root, scene.stage);
		fit();
		new ResizeObserver(fit).observe(root);
		root.replaceChildren(scene.stage.svg);
		if (reduce) {
			scene.tl.seek(scene.still.at);
			scene.stage.render();
			if (cap) cap.textContent = scene.still.cap;
			continue;
		}
		scene.stage.render();
		let started = false;
		new IntersectionObserver(([entry]) => {
			if (!entry.isIntersecting) scene.tl.pause();
			else if (started) scene.tl.resume();
			else {
				started = true;
				scene.tl.play();
			}
		}).observe(root);
	}
}
