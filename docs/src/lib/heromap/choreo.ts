// The hero map, animated: one looping anime.js timeline, `TOTAL` long, that plays the ten chapters of
// `chapters.ts` on the nodes `render.ts` drew. It shows the simulations' events and nothing else: a job flies from
// Add() to land in its lane at its land, leaves at its dispatch or take, and runs its ring's arc until it is done;
// a store's record waits in the tray until its ack lands; the SQLite program dies and restarts when its plan says.
//
// How the timeline is built:
// - The score (`score.ts`) keeps a track of every animated property and the cues that change it, and makes the
//   timeline: every tween with its from-value written out, all in time order, every property set at 0 so each
//   loop starts again from chapter 1 with every piece off, and no property ending in a set. It says why.
// - Lanes. The squares in a lane keep the simulation's order and stay apart: a square lands in the first slot
//   behind the ones waiting that it can reach and sit in untouched, and those waiting move up once their way is
//   clear. One that moves ahead of others goes round them, through the gap below the lane.

import { eases, svg, type Timeline } from 'animejs';
import { CHAPTERS, TOTAL, starts, type Chapter, type Part } from './chapters.ts';
import { poolStates, type PoolEvent } from './pool.ts';
import { END_FADE, type MapNodes } from './render.ts';
import { CONNS, LANE, PIECES, REGIONS, RINGS, RING_R, frame, slotX, slotY, type Box, type Camera } from './scene.ts';
import {
	Score,
	clamp01,
	dist,
	lengthOf,
	pt,
	solve,
	trajectory,
	type Ease,
	type Pt,
	type Stage,
	type Values,
} from './score.ts';
import { queueStates, workers } from './stores.ts';

type Worker = keyof typeof RINGS;
type PoolPart = Extract<Part, { kind: 'pool' }>;
type StorePart = Exclude<Part, PoolPart>;

const CAMERA = 1200; // each chapter's camera move
const DRAW = 650; // a solid outline drawing in
const POP = 600; // a chip or a ring popping in
const FADE = 500; // a dashed part fading in
const LABEL = 400; // a label fading in
const ADD_FLIGHT = 600; // the longest flight from Add() or Publish() into a lane
const QUICK = 250; // the shortest
const GROW = 0.4; // a job's size as it leaves Add() or Publish(): it grows on its way
const FLIGHT = 1000; // the longest flight from a lane to a ring
const SPEED = 0.3; // units per ms along a lane-and-line path
const REFLOW = 380; // waiting squares moving up
const SOON = 0.75 * REFLOW; // a job leaving this soon would stop mid-move: it stays put and leaves from there
const DROP = 420; // a taken record dropping into the tray
const REQUEUE = 520; // records going back to the queue at a restart
const SHRINK = 300; // a job shrinking to fit its ring, as it arrives
const LAGS = [0, 45, 90]; // a job and its two trails
const IN_RING = 0.5; // a job's size in its ring
const CLEAR = 44; // a copy has left the store's mouth, for the next one, once it is two squares away
const NEAR = 16; // a landing square this close to its slot is all but in it: from then on it must not touch
const PULSE = { scale: 1.22, up: 140, down: 380 }; // a ring when its job is done
const EXIT_AFTER = 400; // a goroutine that exits at its job's done leaves once its pulse has settled

const E = {
	move: eases.inOutExpo,
	draw: eases.inOutQuart,
	pop: eases.outExpo,
	fade: eases.outQuad,
	out: eases.inQuad, // leaving: words, fades, a check
	linear: eases.linear,
	signal: eases.inOutQuad,
	leave: eases.outQuad, // a taken record into the tray, and its copy out of the store: at once
	vanish: eases.inBack(2),
	exit: eases.inBack(1.6),
} satisfies Record<string, Ease>;

const NS = 'http://www.w3.org/2000/svg';
const centre = (b: Box) => pt(b.x + b.w / 2, b.y + b.h / 2);

const PIECE = new Map(PIECES.map((p) => [p.id, p]));
const box = (id: string): Box => {
	const p = PIECE.get(id);
	if (!p) throw new Error(`heromap: no piece ${id}`);
	return p.box;
};

/** A line of scene.ts as the points it runs through (its path data is M, H, V and L only). */
function line(id: string): Pt[] {
	const conn = CONNS.find((c) => c.id === id);
	if (!conn) throw new Error(`heromap: no line ${id}`);
	const pts: Pt[] = [];
	for (const [, op, args] of conn.d.matchAll(/([MLHV])([^MLHV]*)/g)) {
		const [a, b] = args.trim().split(/[\s,]+/).map(Number);
		const last = pts[pts.length - 1];
		pts.push(op === 'H' ? pt(a, last.y) : op === 'V' ? pt(last.x, a) : pt(a, b));
	}
	return pts;
}

// ---------------------------------------------------------------------------------------------------------------
// Lanes: the squares waiting in a queue lane, a store's queue, or the topic.

/**
 * Something that waits in a lane: a job with its trails, a published value with its trails, or a record. Squares
 * of one family may overlap: a published value and its copies, a store job's record and its copy.
 */
interface Square {
	id: string;
	family: string;
	els: Element[];
}

const one = (_: number) => 1;

/** Every square of a chapter that can meet another in a lane, to keep them apart. */
class Bodies {
	private readonly list: { el: Element; family: string }[] = [];
	private readonly score: Score;

	constructor(score: Score) {
		this.score = score;
	}

	add(sq: Square) {
		this.list.push({ el: sq.els[0], family: sq.family });
	}

	/**
	 * The first square showing that one of `family` at path(t), of size(t), touches from `from` to `to`, and
	 * when.
	 */
	touch(family: string, path: (t: number) => Pt, from: number, to: number, skip: Element[] = [], size = one) {
		for (let t = from; t <= to; t += 15) {
			const p = path(t);
			for (const b of this.list) {
				if (b.family === family || skip.includes(b.el)) continue;
				const q = this.score.pos(b.el, t);
				if (Math.abs(p.x - q.x) > LANE.slot || Math.abs(p.y - q.y) > LANE.slot) continue;
				if (this.score.at(b.el, 'opacity', t)[0] < 0.1) continue;
				const reach = (LANE.slot / 2) * (size(t) + this.score.at(b.el, 'scale', t)[0]) - 0.5;
				if (Math.abs(p.x - q.x) < reach && Math.abs(p.y - q.y) < reach) return { id: b.family, at: t, p, q };
			}
		}
		return undefined;
	}
}

class Lane {
	/** The squares in the lane, each with the slot it is in or headed for. */
	readonly held = new Map<string, { sq: Square; slot: number }>();
	readonly score: Score;
	readonly agenda: Agenda;
	readonly bodies: Bodies;
	readonly id: string;
	/** The waiting squares in the simulation's order at t, head first. */
	readonly order: (t: number) => string[];
	/**
	 * How soon a square must leave not to move up first. A job flies off slowly (inOutExpo), so one moving up and
	 * then away would stop dead in between; a taken record drops at once, from wherever it is.
	 */
	readonly soon: number;
	readonly b: Box;
	readonly y: number;
	readonly gap: number; // the row below the lane (GAP)

	constructor(score: Score, agenda: Agenda, bodies: Bodies, id: string, order: (t: number) => string[], soon: number) {
		this.score = score;
		this.agenda = agenda;
		this.bodies = bodies;
		this.id = id;
		this.order = order;
		this.soon = soon;
		this.b = box(id);
		this.y = slotY(this.b.y);
		this.gap = GAP[id] ?? this.b.y + LANE.h + LANE.slot / 2;
	}

	x(k: number) {
		return slotX(this.b.x, k);
	}

	slot(k: number) {
		return pt(this.x(k), this.y);
	}

	/**
	 * The way out of the lane from `p` onto the line that starts at `exit`: below the lane when other squares
	 * are between `p` and the lane's head, so as not to pass over them.
	 */
	out(p: Pt, exit: Pt, at: number): Pt[] {
		const blocked = [...this.held.values()].some(({ sq }) => {
			const q = this.score.pos(sq.els[0], at);
			return Math.abs(q.y - p.y) < LANE.slot && q.x > p.x + 1;
		});
		return blocked ? [pt(p.x, this.gap), pt(exit.x, this.gap), exit] : [exit];
	}

	/**
	 * Where a square that lands at `at` goes, and how long its flight is: the first slot behind the squares
	 * waiting that it flies to and then sits in without touching anything, with the longest flight that does so
	 * (`durations`, longest first: a quicker flight can stay clear of a square still leaving). Failing that, the
	 * first slot that it comes into and sits in untouched, passing over a square on the way, at speed.
	 */
	landing(sq: Square, at: number, route: (k: number) => Pt[], durations: (route: Pt[]) => number[]) {
		let met: unknown;
		for (let k = Math.max(-1, ...[...this.held.values()].map((h) => h.slot)) + 1; k < 5; k++) {
			const pts = route(k);
			for (const whole of [true, false]) {
				for (const d of durations(pts)) {
					const start = at - d;
					const grow = (t: number) => GROW + (1 - GROW) * E.move(clamp01((t - start) / d));
					const path = trajectory(pts, start, d, E.move);
					const near = start + d * solve(E.move, 1 - NEAR / lengthOf(pts));
					met = this.bodies.touch(sq.family, path, whole ? start : near, at + REFLOW, [], grow);
					if (!met) return { k, pts, d };
				}
			}
		}
		throw new Error(`heromap: no slot in ${this.id} is free for ${sq.id} at ${at} ms: ${JSON.stringify(met)}`);
	}

	/** A square has landed in slot k (its flight ends at `at`). */
	land(sq: Square, k: number, at: number) {
		this.held.set(sq.id, { sq, slot: k });
		this.agenda.add(at, () => this.reflow(at));
	}

	/** A square leaves the lane at `at`. */
	leave(id: string, at: number) {
		if (!this.held.delete(id)) throw new Error(`heromap: ${id} leaves ${this.id} at ${at} ms, and is not in it`);
		this.agenda.add(at, () => this.reflow(at));
	}

	/**
	 * Moves the waiting squares to their places in the simulation's order, once they are still and their way is
	 * clear. A square moving ahead of others that move back goes round them, below the lane. What blocks the way
	 * may not have left yet (its event is still to come), so a reflow that finds no way looks again later.
	 */
	reflow(at: number, tries = 0) {
		// Each square goes to its place in the simulation's order. One about to leave stays where it is, and the
		// ones after it stay behind it.
		const moves: { held: { sq: Square; slot: number }; to: number }[] = [];
		const staying = new Set(this.order(at + this.soon));
		let next = 0;
		for (const id of this.order(at)) {
			const held = this.held.get(id);
			if (!held) continue;
			if (!staying.has(id)) {
				if (held.slot >= next) next = held.slot + 1;
				continue;
			}
			if (held.slot !== next) moves.push({ held, to: next });
			next++;
		}
		if (moves.length === 0) return;
		// A square that ends up ahead of one that was ahead of it passes it, so it goes round: below the lane.
		const ends = [...this.held.values()].map((h) => ({ h, to: moves.find((m) => m.held === h)?.to ?? h.slot }));
		const passes = ({ held, to }: (typeof moves)[number]) =>
			ends.some((o) => o.h !== held && o.h.slot < held.slot && o.to > to);
		const route = (m: (typeof moves)[number]) => {
			const below = [pt(this.x(m.held.slot), this.gap), pt(this.x(m.to), this.gap)];
			return passes(m) ? [...below, this.slot(m.to)] : [this.slot(m.to)];
		};
		const mains = moves.map(({ held }) => held.sq.els[0]);
		const still = Math.max(at, ...mains.map((el) => this.score.track(el, 'pos').end));
		for (let s = still; s < still + 1000; s += 20) {
			const met = moves.some((m) => {
				const path = trajectory([this.score.pos(m.held.sq.els[0], s), ...route(m)], s, REFLOW, E.move);
				return this.bodies.touch(m.held.sq.family, path, s, s + REFLOW + LAGS[2], mains);
			});
			if (met) continue;
			if (s > at) {
				this.agenda.add(s, () => this.reflow(s, tries));
				return;
			}
			for (const m of moves) {
				glide(this.score, m.held.sq.els, route(m), at, REFLOW);
				m.held.slot = m.to;
			}
			return;
		}
		if (tries < 10) this.agenda.add(still + 500, () => this.reflow(still + 500, tries + 1));
	}
}

/** Moves a square with its trails, each trail a little behind. */
function glide(score: Score, els: Element[], route: Pt[], start: number, duration: number, ease = E.move) {
	els.forEach((el, i) => score.move(el, route, start + LAGS[i], duration, ease));
}

/** Tweens a square and its trails alike, each trail a little behind. */
function each(score: Score, els: Element[], values: Values, start: number, duration: number, ease: Ease) {
	els.forEach((el, i) => score.tween(el, values, start + LAGS[i], duration, ease));
}

/** Actions in time order, and in the order they were added at one instant: later ones can add more. */
class Agenda {
	private items: { at: number; seq: number; run: () => void }[] = [];
	private seq = 0;

	add(at: number, run: () => void) {
		this.items.push({ at, seq: this.seq++, run });
	}

	run() {
		for (let guard = 0; this.items.length > 0; guard++) {
			if (guard > 100000) throw new Error('heromap: the agenda does not settle');
			this.items.sort((a, b) => a.at - b.at || a.seq - b.seq);
			this.items.shift()!.run();
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------
// Routes: where jobs, records and signals go, along the lines of scene.ts.

const LOOP = centre(box('loop'));

// The core's Add() feeds the priority lane and the third lane too: its jobs turn up or down between the chip and
// the lanes.
const ADD = centre(box('add'));
const TURN = (box('add').x + box('add').w + box('queue').x) / 2;

/** From Add() or Publish() to a lane's tail end, on its slots' row. */
const ENTRY: Record<string, Pt[]> = {
	queue: [ADD, ...line('add-to-queue')],
	prio: [ADD, pt(TURN, ADD.y), pt(TURN, slotY(box('prio').y))],
	'queue-c': [ADD, pt(TURN, ADD.y), pt(TURN, slotY(box('queue-c').y))],
	topic: [centre(box('publish')), ...line('publish-to-topic')],
	'r-queue': [centre(box('r-add')), ...line('r-add-to-r-queue')],
	's-queue': [centre(box('s-add')), ...line('s-add-to-s-queue')],
	'r-queue2': [centre(box('a-add')), ...line('a-add-to-r-queue2')],
};

/**
 * The row below a lane, halfway to the lane or tray under it: where a waiting square goes round the others, and
 * where a record crosses between a store's queue and its tray.
 */
const GAP: Record<string, number> = {
	prio: (box('prio').y + LANE.h + box('queue').y) / 2,
	's-queue': (box('s-queue').y + LANE.h + box('s-tray').y) / 2,
	'r-queue2': (box('r-queue2').y + LANE.h + box('r-tray').y) / 2,
};

/** A rail inside a card, halfway between its rings and its bottom edge. */
const rail = (worker: Worker) => (RINGS[worker][0].y + RING_R + box(worker).y + box(worker).h) / 2;

/** Whether a worker's ring i is shown. */
type Shown = (i: number) => boolean;

/**
 * Into a card to one of its rings, from where its line meets the card's edge. A job never crosses another ring
 * shown on its way: one that would goes along a rail below the rings, and up into its own.
 */
function intoCard(worker: Worker, i: number, edge: Pt, shown: Shown): Pt[] {
	const ring = RINGS[worker][i];
	const card = box(worker);
	if (edge.y > card.y + card.h - 4) return [pt(edge.x, ring.y), ring]; // up from below, between the rings
	const between = RINGS[worker].some((r, j) => shown(j) && r.x < ring.x - 1 && r.x > edge.x);
	if (!between) return [ring];
	const side = (card.x + Math.min(...RINGS[worker].map((r) => r.x)) - RING_R) / 2;
	return [pt(side, edge.y), pt(side, rail(worker)), pt(ring.x, rail(worker)), ring];
}

const last = (pts: Pt[]) => pts[pts.length - 1];

/** A pool job from its lane to its ring: through the event loop for the core. */
function toRing(worker: Worker, source: string, i: number, shown: Shown): Pt[] {
	if (worker === 'core') {
		const lead = line(`${source}-to-loop`);
		const out = line('loop-to-core');
		return [...lead, LOOP, ...out, ...intoCard('core', i, last(out), shown)];
	}
	const lead = line(worker === 'second' ? 'topic-to-second' : 'r-queue-to-result');
	return [...lead, ...intoCard(worker, i, last(lead), shown)];
}

// The line a topic subscriber's copies take from the topic.
const LEADS: Record<string, string> = { core: 'topic-to-loop', second: 'topic-to-second' };
const INBOX = 20; // how far short of its line's end a busy subscriber's copy waits

/** Where a busy subscriber's copy waits for a goroutine: on its line from the topic, INBOX short of the end. */
function inbox(lead: Pt[]): Pt {
	const [a, b] = lead.slice(-2);
	const k = INBOX / dist(a, b);
	return pt(b.x + (a.x - b.x) * k, b.y + (a.y - b.y) * k);
}

/**
 * A store job's copy from where its queue's line leaves the store to its consumer's ring. The line to each Redis
 * consumer turns up or down at a bend: a copy for the far ring leaves it there, for the rail below the rings.
 */
function toStoreRing(worker: Worker, i: number, shown: Shown): Pt[] {
	const toB = line('r-queue2-to-redis-b');
	const toC = [toB[0], ...line('r-queue2-to-redis-c')]; // C's line branches off B's
	const lead = worker === 'sqlite' ? line('s-queue-to-sqlite') : worker === 'redis-b' ? toB : toC;
	const into = intoCard(worker, i, last(lead), shown);
	if (worker === 'sqlite' || into.length === 1) return [...lead, ...into];
	const [mouth, bend] = lead;
	const ring = RINGS[worker][i];
	return [mouth, bend, pt(bend.x, rail(worker)), pt(ring.x, rail(worker)), ring];
}

/** An ack from a ring back to its record's tray slot. */
function ackRoute(worker: Worker, i: number, tray: Pt): Pt[] {
	const ring = RINGS[worker][i];
	const bottom = pt(ring.x, ring.y + RING_R);
	if (worker === 'sqlite') {
		const ack = line('sqlite-to-s-tray');
		return [bottom, pt(ring.x, rail(worker)), pt(ack[0].x, rail(worker)), ...ack, tray];
	}
	const ack = line(`${worker}-to-r-tray`);
	return [bottom, pt(ring.x, ack[0].y), ...ack, tray];
}

/** What the choreographer makes on the page as it goes: the score's motion paths and outlines, and signal dots. */
export interface Page extends Stage {
	/** A new signal dot of `kind` (notify, ack, value or error) in the world, hidden. */
	dot(kind: string): Element;
}

/**
 * The page in the browser: motion along hidden route paths in the world (anime.js measures their length, so they
 * are drawn, with neither fill nor stroke), anime.js's drawable outlines, and dots appended to the world.
 */
function browserPage(world: SVGGElement): Page {
	const layer = document.createElementNS(NS, 'g');
	layer.setAttribute('fill', 'none');
	layer.setAttribute('stroke', 'none');
	world.append(layer);
	const routes = new Map<string, SVGPathElement>();
	return {
		path(pts) {
			const d = 'M' + pts.map((p) => `${+p.x.toFixed(3)} ${+p.y.toFixed(3)}`).join('L');
			let path = routes.get(d);
			if (!path) {
				path = document.createElementNS(NS, 'path');
				path.setAttribute('d', d);
				layer.append(path);
				routes.set(d, path);
			}
			const { translateX, translateY } = svg.createMotionPath(path)!;
			return { translateX, translateY };
		},
		// anime.js draws an outline blank as it makes its stand-in, unless its pathLength is already anime's 1000:
		// so set, building the timeline leaves the still frame whole.
		drawable: (el) => {
			el.setAttribute('pathLength', '1000');
			return svg.createDrawable(el)[0];
		},
		dot(kind) {
			const dot = document.createElementNS(NS, 'circle');
			dot.setAttribute('class', `vm-sig vm-sig-${kind}`);
			dot.setAttribute('r', '4');
			dot.setAttribute('opacity', '0');
			world.append(dot);
			return dot;
		},
	};
}

// ---------------------------------------------------------------------------------------------------------------
// The choreographer.

type Lanes = (id: string, order: (t: number) => string[]) => Lane;
type Dispatch = Extract<PoolEvent, { type: 'dispatch' }>;

/** The topic lane of a chapter, when each value's copies leave it, and how many of them have still to go. */
interface Topic {
	lane: Lane;
	fan: Map<string, number>;
	left: Map<string, number>;
}

class Choreo {
	readonly nodes: MapNodes;
	readonly page: Page;
	readonly score: Score;
	readonly cam: Camera;
	/** Rings shown, as `<worker>:<place>`. */
	private readonly shown = new Set<string>();
	/** Rings on their way out, by when they are gone. */
	private readonly leaving = new Map<string, number>();
	/** When each worker's topic inbox is free again: Infinity while a copy waits there. */
	private readonly inboxes = new Map<string, number>();
	/** The squares of the chapter being written. */
	private bodies!: Bodies;

	constructor(nodes: MapNodes, page: Page) {
		this.nodes = nodes;
		this.page = page;
		this.score = new Score(TOTAL, page);
		// The loop opens on chapter 1's frame: from the finale's, the move in would cross an empty map.
		this.cam = { ...frame(REGIONS[0]) };
		this.declareStarts();
	}

	/** Every animated property and where it starts: the world empty, every piece off, every job hidden. */
	private declareStarts() {
		const { score, nodes } = this;
		score.start(this.cam, { x: this.cam.x, y: this.cam.y, s: this.cam.s });
		score.start(nodes.world, { opacity: 0 });
		for (const p of PIECES) this.declarePiece(p.id, p.kind);
		for (const c of CHAPTERS) for (const chip of c.chips ?? []) this.declarePiece(`${chip.id}@${c.n}`, 'chip');
		for (const c of CONNS) {
			const g = nodes.piece(c.id);
			score.start(g, { opacity: c.kind === 'line' ? 1 : 0 });
			if (c.kind === 'line') score.start(this.outline(g), { draw: [0, 0] });
		}
		const prog = nodes.piece('s-prog');
		for (const crash of ['.vm-flash', '.vm-died', '.vm-restarted']) {
			score.start(prog.querySelector(crash)!, { opacity: 0 });
		}
		score.start(this.loop(), { opacity: 1 });
		score.start(this.pulse(), { scale: 1, opacity: 0 });
		for (const [worker, places] of Object.entries(RINGS)) {
			places.forEach((_, i) => {
				const r = nodes.ring(worker, i);
				score.start(r.ring, { scale: 0, opacity: 1 });
				score.start(r.ghost, { opacity: 0 });
				score.start(r.progress, { opacity: 1 });
				score.start(r.check, { opacity: 1 });
				score.start(score.drawable(r.progress), { draw: [0, 0] });
				score.start(score.drawable(r.check), { draw: [0, 0] });
			});
		}
		for (const c of CHAPTERS) {
			c.captions.forEach((_, i) => {
				for (const word of nodes.words(c.n, i)) score.start(word, { opacity: 0, translateY: 10 });
			});
		}
	}

	private declarePiece(key: string, kind: string) {
		const { score } = this;
		const g = this.nodes.piece(key);
		if (kind === 'chip') {
			score.start(g, { opacity: 0, scale: 0 });
			return;
		}
		score.start(g, { opacity: kind === 'tray' ? 0 : 1 });
		if (kind === 'tray') return;
		// Off, a piece shows nothing: its outline undrawn and its fill clear.
		score.start(this.outline(g), { draw: [0, 0] });
		score.start(g.querySelector('.vm-shape')!, { fillOpacity: 0 });
		const label = g.querySelector(':scope > .vm-label');
		if (label) score.start(label, { opacity: 0 });
		for (const slot of g.querySelectorAll('.vm-slot')) score.start(slot, { opacity: 0 });
	}

	/** A piece's or a line's outline, to draw. */
	private outline(g: Element): object {
		return this.score.drawable(g.querySelector('.vm-shape')!);
	}

	private loop(): Element {
		return this.nodes.piece('loop').querySelector('.vm-shape')!;
	}

	private pulse(): Element {
		return this.nodes.piece('loop').querySelector('.vm-pulse')!;
	}

	/** A job with its trails, hidden until it first moves. A copy of a published value is of its value's family. */
	private job(id: string, family = id): Square {
		const els = this.nodes.job(id);
		for (const el of els) this.score.start(el, { pos: [0, 0], opacity: 0, scale: 1 });
		const sq = { id, family, els };
		this.bodies.add(sq);
		return sq;
	}

	/** A store job's record: solid while it waits, an outline in flight. */
	private record(id: string): Square {
		const el = this.nodes.record(id);
		this.score.start(el, { pos: [0, 0], opacity: 0, scale: 1 });
		this.score.start(this.fill(el), { fillOpacity: 1 });
		const sq = { id, family: id, els: [el] };
		this.bodies.add(sq);
		return sq;
	}

	private fill(record: Element): Element {
		return record.querySelector('.vm-jb')!;
	}

	/** A dot that runs along `route` from `start` for `duration`: a notify, an ack, a value or an error. */
	private signal(kind: string, route: Pt[], start: number, duration: number) {
		const dot = this.page.dot(kind);
		const { score } = this;
		score.start(dot, { pos: [route[0].x, route[0].y], opacity: 0 });
		score.place(dot, route[0], start);
		score.tween(dot, { opacity: 1 }, start, 80, E.linear);
		score.move(dot, route.slice(1), start, duration, E.signal);
		score.tween(dot, { opacity: 0 }, start + duration, 120, E.linear);
	}

	build(): Timeline {
		const at = starts();
		this.score.tween(this.nodes.world, { opacity: 1 }, 0, 450, E.linear);
		for (const c of CHAPTERS) this.chapter(c, at[c.n - 1]);
		this.score.tween(this.nodes.world, { opacity: 0 }, TOTAL - END_FADE.duration, END_FADE.duration, E.out);
		return this.score.build(() => this.nodes.setCamera(this.cam));
	}

	private chapter(c: Chapter, T: number) {
		const { score } = this;
		// Chapter 1 has no camera move: the camera starts on its frame, so its pieces draw in at once.
		const move = c.n === 1 ? 0 : CAMERA;
		if (move) score.tween(this.cam, { ...frame(REGIONS[c.n - 1]) }, T, move, E.move);
		this.open(c, T + move);
		this.captions(c, T);
		for (const chip of c.chips ?? []) {
			const g = this.nodes.piece(`${chip.id}@${c.n}`);
			score.tween(g, { opacity: 1, scale: 1 }, T + chip.at, POP, E.pop);
			score.tween(g, { opacity: 0, scale: 0 }, T + chip.until, 300, E.out);
		}

		// Everything the chapter's parts and published values do, in time order.
		const agenda = new Agenda();
		const lanes = new Map<string, Lane>();
		this.bodies = new Bodies(score);
		const lane: Lanes = (id, order) => {
			let found = lanes.get(id);
			const soon = id === 's-queue' || id === 'r-queue2' ? 0 : SOON;
			if (!found) lanes.set(id, (found = new Lane(score, agenda, this.bodies, id, order, soon)));
			return found;
		};
		const topic = this.topic(c, T, agenda, lane);
		for (const part of c.parts) {
			if (part.kind === 'pool') this.pool(c, part, T, agenda, lane, topic);
			else this.store(part, T, agenda, lane);
		}
		agenda.run();
	}

	/** The chapter's new pieces switch on once the camera is there, with the idle rings its workers start with. */
	private open(c: Chapter, t0: number) {
		const { score, nodes } = this;
		PIECES.filter((p) => p.chapter === c.n).forEach((p, i) => {
			const g = nodes.piece(p.id);
			const t = t0 + 40 * i;
			if (p.kind === 'chip') {
				score.tween(g, { opacity: 1, scale: 1 }, t, POP, E.pop);
				return;
			}
			if (p.kind === 'tray') {
				score.tween(g, { opacity: 1 }, t, FADE, E.fade);
				return;
			}
			score.tween(this.outline(g), { draw: [0, 1] }, t, DRAW, E.draw);
			score.tween(g.querySelector('.vm-shape')!, { fillOpacity: 1 }, t, DRAW, E.draw);
			const label = g.querySelector(':scope > .vm-label');
			if (label) score.tween(label, { opacity: 1 }, t + 250, LABEL, E.fade);
			// A lane's slots appear from its head.
			g.querySelectorAll('.vm-slot').forEach((slot, k) => {
				score.tween(slot, { opacity: 1 }, t + 200 + 40 * k, 300, E.fade);
			});
		});
		CONNS.filter((l) => l.chapter === c.n).forEach((l, j) => {
			const g = nodes.piece(l.id);
			const t = t0 + 120 + 40 * j;
			if (l.kind === 'line') score.tween(this.outline(g), { draw: [0, 1] }, t, DRAW, E.draw);
			else score.tween(g, { opacity: 1 }, t + 80, FADE, E.fade);
		});
		for (const part of c.parts) {
			const crews = part.kind === 'pool' ? [{ ...part.plan, id: part.worker }] : workers(part.plan);
			for (const w of crews) {
				const name = part.kind === 'pool' ? part.worker : consumer(w.id);
				for (const i of w.idle) this.spawn(name, i, t0 + 300);
				this.ghosts(name, w.limit, t0 + 300);
			}
		}
	}

	/** Which of a worker's rings are on screen at `at`: shown, or not yet gone. */
	private showing(worker: string, at: number): Shown {
		return (i) => this.shown.has(`${worker}:${i}`) || (this.leaving.get(`${worker}:${i}`) ?? 0) > at;
	}

	/** Pops a goroutine's ring in, unless it is shown already. */
	private spawn(worker: string, i: number, at: number) {
		const key = `${worker}:${i}`;
		if (this.shown.has(key)) return;
		this.shown.add(key);
		this.score.tween(this.nodes.ring(worker, i).ring, { scale: 1 }, at, POP, E.pop);
	}

	/** The dashed places of a pool's goroutines, up to its limit, fade in where they are not shown yet. */
	private ghosts(worker: string, limit: number, at: number) {
		for (let i = 0; i < limit; i++) {
			const { ghost } = this.nodes.ring(worker, i);
			const track = this.score.track(ghost, 'opacity');
			if (track.at(at)[0] === 0 && track.end <= at) this.score.tween(ghost, { opacity: 1 }, at, FADE, E.fade);
		}
	}

	/** Words rise in 55 ms apart, each caption once the one before has gone, and leave 30 ms apart. */
	private captions(c: Chapter, T: number) {
		let free = 0; // when the previous caption has left
		c.captions.forEach((cap, i) => {
			const words = this.nodes.words(c.n, i);
			const enter = Math.max(cap.at, free);
			this.score.stagger(words, { opacity: 1, translateY: 0 }, T + enter, 55, 520, E.pop);
			this.score.stagger(words, { opacity: 0, translateY: -8 }, T + cap.until, 30, 300, E.out);
			free = cap.until + 300 + 30 * words.length;
		});
	}

	// -- Pools --------------------------------------------------------------------------------------------------

	/**
	 * A topic is not a queue: Publish wakes every subscriber at once, so all of a value's copies leave the topic
	 * together, a wake-up after it lands. A subscriber with no goroutine free keeps its copy at its inbox until one is.
	 */
	private topic(c: Chapter, T: number, agenda: Agenda, lane: Lanes): Topic | undefined {
		const values = c.published ?? [];
		if (values.length === 0) return undefined;
		const copies = c.parts.flatMap((p) => (p.kind === 'pool' ? p.events : [])).filter(isTopicDispatch);
		const of = (id: string) => copies.filter((e) => valueOf(e.job) === id);
		const subscribed = c.parts.filter((p) => p.kind === 'pool' && p.plan.sources.some((s) => s.id === 'topic'));
		const wake = Math.min(...subscribed.map((p) => (p as PoolPart).plan.wake));
		const fan = new Map(values.map((v) => [v.id, T + v.at + wake]));
		const left = new Map(values.map((v) => [v.id, of(v.id).length]));
		const waiting = (t: number) => values.filter((v) => T + v.at <= t && fan.get(v.id)! > t).map((v) => v.id);
		const topic = lane('topic', waiting);
		for (const v of values) {
			const sq = this.job(v.id);
			agenda.add(T + v.at, () => this.arrive(topic, sq, 'topic', T + v.at, ADD_FLIGHT));
		}
		return { lane: topic, fan, left };
	}

	/**
	 * A square flies in from Add() or Publish() and lands at `at` in the first free slot: `flight` long if `exact`
	 * (a Redis job's `landAfter`), and otherwise as long as its path at SPEED, up to `flight`. It grows as it goes.
	 */
	private arrive(l: Lane, sq: Square, entry: string, at: number, flight: number, exact = false) {
		const { score } = this;
		const route = (k: number) => [...ENTRY[entry], l.slot(k)];
		const durations = (pts: Pt[]) => {
			if (exact) return [flight];
			const d = Math.min(flight, Math.max(QUICK, lengthOf(pts) / SPEED));
			return [...new Set([d, d - 100, d - 200, d - 300, QUICK].filter((x) => x >= QUICK))];
		};
		const { k, pts, d } = l.landing(sq, at, route, durations);
		const start = at - d;
		for (const el of sq.els) score.place(el, pts[0], start);
		each(score, sq.els, { opacity: 1 }, start, 150, E.linear);
		for (const el of sq.els) score.set(el, { scale: GROW }, start);
		each(score, sq.els, { scale: 1 }, start, d, E.move);
		glide(score, sq.els, pts.slice(1), start, d);
		l.land(sq, k, at);
	}

	private pool(c: Chapter, part: PoolPart, T: number, agenda: Agenda, lane: Lanes, topic: Topic | undefined) {
		const states = poolStates(part.plan, part.events);
		const order = (source: string) => (t: number) => {
			let found: string[] = [];
			for (const s of states) {
				if (T + s.at > t) break;
				found = s.waiting[source] ?? [];
			}
			return found;
		};
		const squares = new Map<string, Square>();
		const done = new Map(part.events.flatMap((e) => (e.type === 'done' ? [[e.job, T + e.at] as const] : [])));

		part.events.forEach((e, k) => {
			const at = T + e.at;
			switch (e.type) {
				case 'land': {
					if (e.source === 'topic') return; // its published value stands for it in the lane
					const sq = this.job(e.job);
					squares.set(e.job, sq);
					agenda.add(at, () => this.arrive(lane(e.source, order(e.source)), sq, e.source, at, ADD_FLIGHT));
					return;
				}
				case 'dispatch': {
					const fan = e.source === 'topic' ? topic!.fan.get(valueOf(e.job))! : at;
					if (fan < at) {
						// The worker is busy: its copy leaves with the others and waits at its inbox.
						agenda.add(fan, () => {
							this.toInbox(part.worker, this.copy(e, fan, topic!, squares), fan, topic!.lane);
							this.published(e, fan, topic!);
						});
						agenda.add(at, () => this.dispatch(part.worker, e, at, done.get(e.job)!, squares.get(e.job)!));
						return;
					}
					agenda.add(at, () => {
						const from = e.source === 'topic' ? topic!.lane : lane(e.source, order(e.source));
						const sq = e.source === 'topic' ? this.copy(e, at, topic!, squares) : squares.get(e.job)!;
						if (e.source !== 'topic') from.leave(e.job, at);
						this.dispatch(part.worker, e, at, done.get(e.job)!, sq, from);
						if (e.source === 'topic') this.published(e, at, topic!);
					});
					return;
				}
				case 'done':
					agenda.add(at, () => this.done(part.worker, e.ring, at, squares.get(e.job)!, part.outcomes?.[e.job]));
					return;
				case 'exit': {
					// A goroutine that exits as its job is done leaves once its ring's pulse has settled.
					const ends = (d: PoolEvent) => d.type === 'done' && d.ring === e.ring && d.at === e.at;
					const asDone = part.events.slice(0, k).some(ends);
					agenda.add(at, () => this.exit(part.worker, e.ring, asDone ? at + EXIT_AFTER : at));
					return;
				}
				case 'control':
					agenda.add(at, () => this.control(c, part, e, at));
					return;
			}
		});
	}

	/** A subscriber's copy of a published value appears on the value, to leave from there. */
	private copy(e: Dispatch, at: number, topic: Topic, squares: Map<string, Square>): Square {
		const value = topic.lane.held.get(valueOf(e.job));
		if (!value) throw new Error(`heromap: ${e.job} leaves at ${at} ms, and its value is not in the topic`);
		const sq = this.job(e.job, value.sq.id);
		squares.set(e.job, sq);
		const p = this.score.pos(value.sq.els[0], at);
		for (const el of sq.els) {
			this.score.place(el, p, at);
			this.score.set(el, { opacity: 1 }, at);
		}
		return sq;
	}

	/** Once its last copy has left, the published value goes too. */
	private published(e: Dispatch, at: number, topic: Topic) {
		const id = valueOf(e.job);
		const left = topic.left.get(id)! - 1;
		topic.left.set(id, left);
		if (left > 0) return;
		const value = topic.lane.held.get(id)!;
		for (const el of value.sq.els) this.score.set(el, { opacity: 0 }, at);
		topic.lane.leave(id, at);
	}

	/** A busy subscriber's copy flies out of the topic with the others and waits at the end of its line. */
	private toInbox(worker: Worker, sq: Square, at: number, from: Lane) {
		const { score } = this;
		const lead = line(LEADS[worker]);
		if ((this.inboxes.get(worker) ?? 0) > at) throw new Error(`heromap: two copies would wait at ${worker}'s inbox`);
		const here = score.pos(sq.els[0], at);
		const route = [...from.out(here, lead[0], at), ...lead.slice(1, -1), inbox(lead)];
		glide(score, sq.els, route, at, Math.min(FLIGHT, Math.max(350, lengthOf([here, ...route]) / SPEED)));
		this.inboxes.set(worker, Infinity);
	}

	/** The job flies from where it waits, a lane or its inbox, to its ring: at most FLIGHT and shorter than its work. */
	private dispatch(worker: Worker, e: Dispatch, at: number, end: number, sq: Square, from?: Lane) {
		const { score } = this;
		const here = score.pos(sq.els[0], at);
		const toIt = toRing(worker, e.source, e.ring, this.showing(worker, at));
		const [exit, ...rest] = toIt;
		let route: Pt[];
		if (from) route = [...from.out(here, exit, at), ...rest];
		else {
			route = toIt.slice(line(LEADS[worker]).length - 1); // on from the inbox, at the end of the line
			this.inboxes.set(worker, at);
		}
		const length = lengthOf([here, ...route]);
		const flight = Math.min(FLIGHT, end - at - 300, Math.max(350, length / SPEED));
		if (e.spawned) this.spawn(worker, e.ring, at);
		glide(score, sq.els, route, at, flight);
		each(score, sq.els, { scale: IN_RING }, at + flight - SHRINK, SHRINK, E.fade);
		if (worker === 'core') {
			// The event loop pulses as the job goes through it.
			const toLoop = lengthOf([here, ...route.slice(0, route.indexOf(LOOP) + 1)]);
			const t = at + solve(E.move, toLoop / length) * flight;
			score.tween(this.pulse(), { scale: 1.9, opacity: 0 }, t, 700, E.fade, { scale: 1, opacity: 0.8 });
		}
		this.run(worker, e.ring, at + flight, end);
	}

	/** A ring's arc fills from the job's arrival until it is done; when its program dies first, it stops there. */
	private run(worker: string, i: number, start: number, end: number, cut = end) {
		const arc = this.score.drawable(this.nodes.ring(worker, i).progress);
		const stop = Math.min(end, cut);
		const to = (stop - start) / (end - start);
		this.score.tween(arc, { draw: [0, to] }, start, stop - start, E.linear, { draw: [0, 0] });
	}

	/** The job is done: it goes, its ring pulses, and a check draws and fades. */
	private done(worker: string, i: number, at: number, sq: Square, outcome?: 'value' | 'error') {
		const { score, nodes } = this;
		const r = nodes.ring(worker, i);
		each(score, sq.els, { scale: 0, opacity: 0 }, at, 260, E.vanish);
		score.tween(r.ring, { scale: PULSE.scale }, at, PULSE.up, E.fade);
		score.tween(r.ring, { scale: 1 }, at + PULSE.up, PULSE.down, E.pop);
		const check = score.drawable(r.check);
		score.tween(check, { draw: [0, 1] }, at + 60, 260, E.fade, { draw: [0, 0] });
		score.tween(check, { draw: [1, 1] }, at + 640, 260, E.out);
		score.tween(score.drawable(r.progress), { draw: [1, 1] }, at + 120, 300, E.out);
		if (outcome) {
			// A result worker hands back a value or an error: a dot takes it to its chip.
			const chip = box(`r-${outcome}`);
			const ring = RINGS.result[i];
			const into = pt(chip.x + 12, chip.y + chip.h / 2);
			const route = [pt(ring.x + RING_R, ring.y), ...line(`result-to-r-${outcome}`), into];
			this.signal(outcome, route, at, 420);
			this.press(nodes.piece(`r-${outcome}`), at + 420);
		}
	}

	private exit(worker: string, i: number, at: number) {
		this.shown.delete(`${worker}:${i}`);
		this.leaving.set(`${worker}:${i}`, at + 450);
		this.score.tween(this.nodes.ring(worker, i).ring, { scale: 0 }, at, 450, E.exit);
	}

	/** A chip pressed: a control called, or a chip taking a result. */
	private press(chip: Element, at: number) {
		this.score.tween(chip, { scale: 1.15 }, at, 150, E.fade);
		this.score.tween(chip, { scale: 1 }, at + 150, 350, E.pop);
	}

	private control(c: Chapter, part: PoolPart, e: Extract<PoolEvent, { type: 'control' }>, at: number) {
		if (e.op === 'tune') {
			// The tune control is on the map from chapter 9; chapter 2's TunePool(3) is a chip of its own (`chips`).
			if (PIECE.get('ctl-tune')!.chapter <= c.n) this.press(this.nodes.piece('ctl-tune'), at);
			this.ghosts(part.worker, e.to!, at + 200);
			return;
		}
		this.press(this.nodes.piece(`ctl-${e.op}`), at);
		// The event loop hands out nothing while the worker is paused, and nothing more once it is stopped.
		this.score.tween(this.loop(), { opacity: e.op === 'resume' ? 1 : 0.35 }, at, 300, E.fade);
	}

	// -- Stores -------------------------------------------------------------------------------------------------

	private store(part: StorePart, T: number, agenda: Agenda, lane: Lanes) {
		const { score } = this;
		const sqlite = part.kind === 'sqlite';
		const states = queueStates(part.events);
		const order = (t: number) => {
			let found: string[] = [];
			for (const s of states) {
				if (T + s.at > t) break;
				found = s.order;
			}
			return found;
		};
		const queue = lane(sqlite ? 's-queue' : 'r-queue2', order);
		const trayBox = box(sqlite ? 's-tray' : 'r-tray');
		const tray = (k: number) => pt(slotX(trayBox.x, k), slotY(trayBox.y));
		const records = new Map<string, Square>();
		const copies = new Map<string, Square>();
		const crash = part.events.find((e) => e.type === 'crash');
		let mouth = 0; // when the store's mouth is clear for the next copy

		part.events.forEach((e, k) => {
			const at = T + e.at;
			switch (e.type) {
				case 'land': {
					const rec = this.record(e.job);
					records.set(e.job, rec);
					copies.set(e.job, this.job(e.job));
					// A SQLite row is written as Add() returns; a Redis job lands `landAfter` after program A adds it.
					const flight = part.kind === 'redis' ? part.plan.landAfter : ADD_FLIGHT;
					agenda.add(at, () => this.arrive(queue, rec, sqlite ? 's-queue' : 'r-queue2', at, flight, !sqlite));
					return;
				}
				case 'notify':
					if (part.kind !== 'redis') return;
					agenda.add(at, () => {
						const travel = part.plan.notifyTravel;
						this.signal('notify', line(`r-store-to-redis-${e.consumer}`), at - travel, travel);
					});
					return;
				// A store's worker is a pool: a goroutine pops in for a take with none idle, or as its program restarts
				// (its stage fades back in over it), and one leaves after its ack, or with its program.
				case 'spawn':
					agenda.add(at, () => this.spawn(consumer(e.consumer), e.ring, at));
					return;
				case 'exit':
					agenda.add(at, () => this.exit(consumer(e.consumer), e.ring, at));
					return;
				case 'take': {
					const end = part.events.find((d, j) => j > k && d.type === 'done' && d.job === e.job);
					const work = part.plan.jobs.find((j) => j.id === e.job)!.work;
					agenda.add(at, () => {
						// The record drops into the tray and is an outline while it is in flight.
						const rec = records.get(e.job)!.els[0];
						const from = score.pos(rec, at);
						const to = tray(e.tray);
						const drop = from.x === to.x ? [to] : [pt(from.x, queue.gap), pt(to.x, queue.gap), to];
						score.move(rec, drop, at, DROP, E.leave);
						score.tween(this.fill(rec), { fillOpacity: 0 }, at + 150, 350, E.fade);
						// A copy comes out where the queue's line leaves the store, one copy at a time, and flies to its ring.
						const copy = copies.get(e.job)!.els;
						const route = toStoreRing(consumer(e.consumer), e.ring, this.showing(consumer(e.consumer), at));
						const out = Math.max(at, mouth);
						for (const el of copy) {
							score.place(el, route[0], out);
							score.set(el, { scale: 1 }, out);
						}
						each(score, copy, { opacity: 1 }, out, 80, E.linear);
						const arrive = out + part.plan.takeTravel;
						glide(score, copy, route.slice(1), out + 60, arrive - out - 60, E.leave);
						each(score, copy, { scale: IN_RING }, arrive - SHRINK, SHRINK, E.fade);
						mouth = out + 60;
						while (dist(score.pos(copy[0], mouth), route[0]) < CLEAR) mouth += 10;
						// A run its program's crash cuts short fills its arc at its work's pace, up to the crash.
						if (end) this.run(consumer(e.consumer), e.ring, arrive, T + end.at);
						else this.run(consumer(e.consumer), e.ring, arrive, arrive + work, T + crash!.at);
						queue.leave(e.job, at);
					});
					return;
				}
				case 'done':
					agenda.add(at, () => this.done(consumer(e.consumer), e.ring, at, copies.get(e.job)!));
					return;
				case 'ack': {
					const taken = part.events.slice(0, k).findLast((d) => d.type === 'take' && d.job === e.job);
					if (taken?.type !== 'take') throw new Error(`heromap: ${e.job} is acked at ${e.at} ms but never taken`);
					agenda.add(at, () => {
						// The ack leaves the ring as the job's function returns and lands as the record is deleted.
						const travel = part.plan.ackTravel;
						this.signal('ack', ackRoute(consumer(taken.consumer), taken.ring, tray(e.tray)), at - travel, travel);
						const rec = records.get(e.job)!.els[0];
						score.tween(rec, { scale: 1.4 }, at, 80, E.fade);
						score.tween(rec, { scale: 0, opacity: 0 }, at + 80, 160, E.out);
					});
					return;
				}
				case 'crash':
					agenda.add(at, () => this.crash(at, copies));
					return;
				case 'restart':
					agenda.add(at, () => this.restart(at));
					return;
				case 'requeue': {
					const after = states[k].order;
					agenda.add(at, () => this.requeue(queue, e.jobs, after, at, records));
					return;
				}
			}
		});
	}

	/**
	 * Opening the queue puts the records in flight back at its head: they rise from the tray as the jobs waiting
	 * move back, all together, and are in the queue as it opens.
	 */
	private requeue(queue: Lane, jobs: string[], after: string[], at: number, records: Map<string, Square>) {
		const { score } = this;
		const start = at - REQUEUE;
		for (const id of jobs) {
			const rec = records.get(id)!;
			const to = queue.slot(after.indexOf(id));
			const p = score.pos(rec.els[0], start);
			score.move(rec.els[0], [pt(p.x, queue.gap), pt(to.x, queue.gap), to], start, REQUEUE, E.move);
			score.tween(this.fill(rec.els[0]), { fillOpacity: 1 }, start + 150, 350, E.fade);
		}
		for (const [id, held] of queue.held) {
			const k = after.indexOf(id);
			glide(score, held.sq.els, [queue.slot(k)], start, REQUEUE);
			held.slot = k;
		}
		for (const id of jobs) queue.held.set(id, { sq: records.get(id)!, slot: after.indexOf(id) });
	}

	/** The SQLite program's stage: its box and labels, Add(), the worker with its rings, and its lines. */
	private stage(): { box: Element; inside: Element[]; lines: Element[] } {
		const { nodes } = this;
		const rings = RINGS.sqlite.flatMap((_, i) => {
			const r = nodes.ring('sqlite', i);
			return [r.ring, r.ghost];
		});
		return {
			box: nodes.piece('s-prog'),
			inside: [nodes.piece('s-add'), nodes.piece('sqlite'), ...rings],
			lines: ['s-add-to-s-queue', 's-queue-to-sqlite', 'sqlite-to-s-tray'].map((id) => nodes.piece(id)),
		};
	}

	/** The program dies mid-job: the view shakes, its jobs vanish, and its stage goes, leaving the file alone. */
	private crash(at: number, copies: Map<string, Square>) {
		const { score, nodes, cam } = this;
		const prog = nodes.piece('s-prog');
		const x = score.at(cam, 'x', at)[0];
		[4, -4, 2, 0].forEach((dx, i) => score.tween(cam, { x: x + dx }, at + 60 * i, i === 3 ? 80 : 60, E.fade));
		const flash = prog.querySelector('.vm-flash')!;
		score.tween(flash, { opacity: 1 }, at, 80, E.linear);
		score.tween(flash, { opacity: 0 }, at + 80, 600, E.fade);
		score.set(prog.querySelector(':scope > .vm-label')!, { opacity: 0 }, at);
		score.set(prog.querySelector('.vm-died')!, { opacity: 1 }, at);
		// The jobs it was running vanish; their records stay in flight in the file.
		for (const copy of copies.values()) {
			if (score.at(copy.els[0], 'opacity', at)[0] > 0) each(score, copy.els, { opacity: 0 }, at + 60, 380, E.out);
		}
		RINGS.sqlite.forEach((_, i) => {
			const r = nodes.ring('sqlite', i);
			score.tween(r.progress, { opacity: 0 }, at + 60, 380, E.out);
			score.tween(r.check, { opacity: 0 }, at + 60, 380, E.out);
		});
		const stage = this.stage();
		for (const el of [stage.box, ...stage.inside, ...stage.lines]) {
			score.tween(el, { opacity: 0 }, at + 700, 650, E.out);
		}
	}

	/** The program boots: its box draws in as PROGRAM · RESTARTED, then what is in it and its lines come back. */
	private restart(at: number) {
		const { score, nodes } = this;
		const stage = this.stage();
		const prog = stage.box;
		const outline = this.outline(prog);
		const shape = prog.querySelector('.vm-shape')!;
		score.set(prog, { opacity: 1 }, at);
		score.set(outline, { draw: [0, 0] }, at);
		score.set(shape, { fillOpacity: 0 }, at);
		score.tween(outline, { draw: [0, 1] }, at, 700, E.draw);
		score.tween(shape, { fillOpacity: 1 }, at, 700, E.draw);
		score.set(prog.querySelector('.vm-died')!, { opacity: 0 }, at);
		score.tween(prog.querySelector('.vm-restarted')!, { opacity: 1 }, at, 300, E.fade);
		RINGS.sqlite.forEach((_, i) => {
			const r = nodes.ring('sqlite', i);
			score.set(score.drawable(r.progress), { draw: [0, 0] }, at);
			score.set(score.drawable(r.check), { draw: [0, 0] }, at);
			score.set(r.progress, { opacity: 1 }, at + 250);
			score.set(r.check, { opacity: 1 }, at + 250);
		});
		for (const el of stage.inside) score.tween(el, { opacity: 1 }, at + 250, 400, E.fade);
		for (const el of stage.lines) score.tween(el, { opacity: 1 }, at + 400, 400, E.fade);
	}
}

/** The card a store's consumer runs its rings in: the SQLite worker's, or Redis's B or C. */
const consumer = (id: string): Worker => (id === 'sqlite' ? 'sqlite' : (`redis-${id}` as Worker));

/** A published value's id from one of its copies' (`<value>-<worker>`). */
const valueOf = (copy: string) => copy.slice(0, copy.lastIndexOf('-'));

const isTopicDispatch = (e: PoolEvent): e is Dispatch => e.type === 'dispatch' && e.source === 'topic';

/**
 * The whole loop as one anime.js timeline: `loop: true`, `TOTAL` long, and paused until played
 * (`autoplay: false`). Each frame puts the camera where its tweens have it. `page` is the browser's unless a test
 * gives its own.
 */
export function buildTimeline(nodes: MapNodes, page: Page = browserPage(nodes.world)): Timeline {
	return new Choreo(nodes, page).build();
}
