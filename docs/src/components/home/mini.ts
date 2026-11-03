// Small pieces of the hero map, for the home page's sections under the story: JOB 02's three scheduling scenes
// (scenes.ts) and JOB 03's storage stage (store.ts). They draw with the map's own classes (`.vm-chip`, `.vm-lane`,
// `.vm-card`, `.vm-go`, `.vm-job` and the rest, styled by heromap/map.css for any `.vm-mini`) and its sizes
// (heromap/scene.ts), so they follow the theme as the map does. The defs they need, the glow and the lanes' edge, are
// in Scenes.astro, once for the page.
//
// A stage holds plain state objects that anime.js tweens (a job's flights, opacity and scale; a ring's progress) and
// writes them to its SVG in `render`, which a timeline calls on every frame. Nothing here touches `window` or reads
// the DOM but `linePx`, which only the page calls: the page's first frame is drawn at build time, into
// heromap/still.ts's small document.

import { createTimeline, eases, type Timeline } from 'animejs';
import { LOGOS } from '../../lib/heromap/render.ts';
import { LABEL_GAP, LANE, RING_R } from '../../lib/heromap/scene.ts';

export type Pt = { x: number; y: number };
export type Ease = (t: number) => number;
export const pt = (x: number, y: number): Pt => ({ x, y });

/** What drawing needs of a document: the page's, or heromap/still.ts's at build time. */
export interface Doc {
	createElementNS(ns: string, tag: string): Element;
}

type Attrs = Record<string, string | number>;

const NS = 'http://www.w3.org/2000/svg';
const DESCENT = 3; // from the bottom of a 14-unit label up to its baseline
const HALF = LANE.slot / 2;
const ARC_R = 15; // a ring's progress arc, as on the map
const ARC = 2 * Math.PI * ARC_R;
const LAGS = [45, 90]; // a moving job's two trails, in ms behind it

export const E = {
	move: eases.inOutExpo,
	pop: eases.outExpo,
	fade: eases.outQuad,
	out: eases.inQuad,
	vanish: eases.inBack(2),
	linear: eases.linear,
} satisfies Record<string, Ease>;

/** How long things take, in ms, shared by the scenes and the stage. */
export const MS = { pop: 450, fly: 650, reflow: 380, fade: 300, pulse: 420 };

export function add(parent: Element, tag: string, attrs: Attrs = {}, text?: string): Element {
	const node = parent.ownerDocument!.createElementNS(NS, tag);
	for (const [name, value] of Object.entries(attrs)) node.setAttribute(name, String(value));
	if (text !== undefined) node.textContent = text;
	parent.append(node);
	return node;
}

const dist = (a: Pt, b: Pt) => Math.hypot(b.x - a.x, b.y - a.y);
const clamp01 = (t: number) => Math.min(1, Math.max(0, t));

/** The point a fraction `f` of the way along a polyline. */
export function along(pts: Pt[], f: number): Pt {
	const total = pts.slice(1).reduce((n, p, i) => n + dist(pts[i], p), 0);
	let d = clamp01(f) * total;
	for (let i = 1; i < pts.length; i++) {
		const seg = dist(pts[i - 1], pts[i]);
		if (d <= seg || i === pts.length - 1) {
			const k = seg ? Math.min(1, d / seg) : 1;
			return pt(pts[i - 1].x + (pts[i].x - pts[i - 1].x) * k, pts[i - 1].y + (pts[i].y - pts[i - 1].y) * k);
		}
		d -= seg;
	}
	return pts[pts.length - 1];
}

/** A move along a route: anime.js tweens `t` from 0 to 1 at a steady pace, and the ease is applied when drawn. */
export interface Flight {
	t: number;
	route: Pt[];
	ms: number;
	ease: Ease;
}

/** A job: a lane slot's square, with its two trails and the text in it. */
export class Job {
	/** Opacity, scale and fill (0 is an outline: a store's record of a job in flight). */
	readonly st = { o: 0, s: 1, f: 1 };
	flights: Flight[] = [];
	home: Pt;
	readonly els: Element[];
	private classes: string[] = [];

	constructor(stage: Stage, at: Pt, text = '', cls = '') {
		this.home = at;
		const square = (c: string) => {
			const g = add(stage.jobs, 'g', { class: `vm-job ${c} ${cls}`.trim(), opacity: 0 });
			add(g, 'rect', { class: 'vm-jb', x: -HALF, y: -HALF, width: LANE.slot, height: LANE.slot, rx: 5 });
			return g;
		};
		const t2 = square('vm-trail vm-t2');
		const t1 = square('vm-trail vm-t1');
		const main = square('vm-main');
		if (text) add(main, 'text', { class: 'vm-num', x: 0, y: 0, 'text-anchor': 'middle', 'dominant-baseline': 'central' }, text);
		this.els = [main, t1, t2];
		this.classes = this.els.map((el) => el.getAttribute('class')!);
		this.undraw = stage.draw(() => this.write());
	}

	private undraw: () => void;

	/** Takes it off the stage. */
	remove() {
		this.undraw();
		for (const el of this.els) el.remove();
	}

	/** Where its square is: along its latest flight under way, or at home. */
	at(lag = 0): Pt {
		for (let i = this.flights.length - 1; i >= 0; i--) {
			const f = this.flights[i];
			if (f.t <= 0) continue;
			return along(f.route, f.ease(clamp01(f.t - lag / f.ms)));
		}
		return this.home;
	}

	/** Whether it is between the ends of a flight, so its trails show. */
	private moving(): boolean {
		for (let i = this.flights.length - 1; i >= 0; i--) {
			const t = this.flights[i].t;
			if (t > 0) return t < 1;
		}
		return false;
	}

	/** Ends its flights where it is, so the next one starts from there. */
	settle() {
		this.home = this.at();
		this.flights = [];
	}

	private write() {
		const { o, s, f } = this.st;
		const trails = this.moving() ? o : 0;
		this.els.forEach((el, i) => {
			const p = this.at(i ? LAGS[i - 1] : 0);
			put(el, 'transform', `translate(${r(p.x)} ${r(p.y)}) scale(${r(s, 3)})`);
			put(el, 'opacity', r(i ? trails : o, 3));
			put(el, 'fill-opacity', r(f, 3));
			put(el, 'class', f < 0.5 ? `${this.classes[i]} vm-out` : this.classes[i]);
		});
	}
}

/** A goroutine's ring on a worker card, over its dashed ghost: its progress arc and its check. */
export class Ring {
	/** Opacity, scale, progress (0 to 1) and the check's opacity. */
	readonly st = { o: 1, s: 1, p: 0, c: 0 };
	readonly at: Pt;

	constructor(stage: Stage, x: number, y: number, on = 1) {
		this.at = pt(x, y);
		this.st.o = on;
		add(stage.world, 'circle', { class: 'vm-ghost', cx: x, cy: y, r: RING_R });
		const ring = add(stage.world, 'g', { class: 'vm-go' });
		add(ring, 'circle', { class: 'vm-ring', cx: x, cy: y, r: RING_R });
		const arc = add(stage.world, 'circle', {
			class: 'vm-progress',
			cx: x,
			cy: y,
			r: ARC_R,
			transform: `rotate(-90 ${x} ${y})`,
		});
		const check = add(stage.world, 'path', { class: 'vm-check', d: `M${x - 4.7} ${y}l3.1 3.5 6.3-7` });
		stage.draw(() => {
			const { o, s, p, c } = this.st;
			put(ring, 'opacity', r(o, 3));
			put(ring, 'transform', `scale(${r(s, 3)})`);
			put(arc, 'stroke-dasharray', `${r(p * ARC, 2)} ${r(ARC, 2)}`);
			put(arc, 'opacity', p > 0.001 ? 1 : 0);
			put(check, 'opacity', r(c, 3));
		});
	}
}

// Attributes are written only when they change.
const written = new WeakMap<Element, Map<string, string>>();
export function put(el: Element, name: string, value: string | number) {
	let seen = written.get(el);
	if (!seen) written.set(el, (seen = new Map()));
	const v = String(value);
	if (seen.get(name) === v) return;
	seen.set(name, v);
	el.setAttribute(name, v);
}
const r = (n: number, digits = 1) => Number(n.toFixed(digits));

/** A lane's (or a tray's) slots: the head on the right. */
export interface Lane {
	g: Element;
	slot(k: number): Pt;
	/** The middle of its right end, where its line starts. */
	end: Pt;
}

/** One small map: an SVG with the map's world in it, and the pieces drawn on it. */
export class Stage {
	readonly svg: Element;
	readonly world: Element;
	/** Its width, in its units. */
	readonly w: number;
	/** Where jobs are drawn: the world, then the layer over every piece (`layer`). */
	jobs: Element;
	private writers = new Set<() => void>();

	constructor(doc: Doc, w: number, h: number) {
		this.w = w;
		this.svg = doc.createElementNS(NS, 'svg');
		for (const [n, v] of Object.entries({ class: 'vm-mini-svg', viewBox: `0 0 ${w} ${h}`, 'aria-hidden': 'true' })) {
			this.svg.setAttribute(n, v);
		}
		this.world = add(this.svg, 'g', { class: 'vm-world', style: '--vm-px: 1' });
		this.jobs = this.world;
	}

	/** Starts the layer the jobs fly in, over everything drawn so far. Call it once the pieces are on. */
	layer() {
		this.jobs = add(this.world, 'g', { class: 'vm-layer' });
	}

	/** Adds a writer that `render` runs. Returns what takes it off again. */
	draw(write: () => void): () => void {
		this.writers.add(write);
		return () => this.writers.delete(write);
	}

	render() {
		for (const write of this.writers) write();
	}

	/** A label, its baseline at y: 14-unit capitals, as the map's. */
	label(parent: Element, x: number, y: number, text: string, attrs: Attrs = {}): Element {
		return add(parent, 'text', { class: 'vm-label', x, y, ...attrs }, text);
	}

	chip(x: number, cy: number, w: number, text: string): Element {
		const g = add(this.world, 'g', { class: 'vm-piece vm-chip' });
		add(g, 'rect', { class: 'vm-shape', x, y: cy - 14, width: w, height: 28, rx: 14 });
		add(g, 'text', { class: 'vm-text', x: x + w / 2, y: cy, 'text-anchor': 'middle', 'dominant-baseline': 'central' }, text);
		return g;
	}

	/**
	 * A queue lane or a dashed tray, the head on the right. Its slots are the map's distance apart, or `step` apart to
	 * leave room for names under them.
	 */
	lane(x: number, y: number, slots: number, label: string, kind: 'lane' | 'tray' = 'lane', step: number = LANE.step): Lane {
		const w = 2 * LANE.pad + LANE.slot + step * (slots - 1);
		const cls = kind === 'lane' ? 'vm-lane' : 'vm-tray vm-dashed';
		const g = add(this.world, 'g', { class: `vm-piece ${cls}` });
		add(g, 'rect', { class: 'vm-shape', x, y, width: w, height: LANE.h, rx: 10 });
		const cy = y + LANE.h / 2;
		const slot = (k: number) => pt(x + w - LANE.pad - HALF - step * k, cy);
		for (let k = 0; k < slots; k++) {
			const s = slot(k);
			add(g, 'rect', { class: 'vm-slot', x: s.x - HALF, y: s.y - HALF, width: LANE.slot, height: LANE.slot, rx: 5 });
		}
		if (label) this.label(g, x, y - LABEL_GAP - DESCENT, label);
		return { g, slot, end: pt(x + w, cy) };
	}

	card(x: number, y: number, w: number, h: number, label: string): Element {
		const g = add(this.world, 'g', { class: 'vm-piece vm-card' });
		add(g, 'rect', { class: 'vm-shape', x, y, width: w, height: h, rx: 14 });
		this.label(g, x, y - LABEL_GAP - DESCENT, label);
		return g;
	}

	/** The event loop: a circle. */
	loop(cx: number, cy: number, radius: number): Element {
		const g = add(this.world, 'g', { class: 'vm-piece vm-loop' });
		add(g, 'circle', { class: 'vm-shape', cx, cy, r: radius });
		return g;
	}

	/** A store (Redis, SQLite) with its logo before its label, as on the map. */
	store(x: number, y: number, w: number, h: number, label: string, logo?: 'r-store' | 's-store'): Element {
		const g = add(this.world, 'g', { class: 'vm-piece vm-store' });
		add(g, 'rect', { class: 'vm-shape', x, y, width: w, height: h, rx: 14 });
		const top = y - LABEL_GAP - DESCENT;
		const mark = logo && LOGOS[logo];
		if (mark) add(g, 'path', { class: `vm-logo vm-logo-${mark.name}`, d: mark.d, transform: `translate(${x} ${top - 11}) scale(0.5)` });
		this.label(g, mark ? x + 17 : x, top, label);
		return g;
	}

	/** A line, as SVG path data drawn the way its traffic goes; an ack line is dashed. */
	line(d: string, kind: 'line' | 'ack' = 'line'): Element {
		const g = add(this.world, 'g', { class: `vm-conn vm-${kind}${kind === 'ack' ? ' vm-dashed' : ''}` });
		add(g, 'path', { class: 'vm-shape', d });
		return g;
	}
}

/** Adds a flight along `route` to a job, at `at` ms on the timeline, and returns it. */
export function fly(tl: Timeline, job: Job, route: Pt[], at: number, ms = MS.fly, ease: Ease = E.move): Flight {
	const f: Flight = { t: 0, route, ms, ease };
	job.flights.push(f);
	tl.add(f, { t: [0, 1], duration: ms, ease: 'linear' }, at);
	return f;
}

/**
 * The light map's line floor (map.css): sets --vm-px, a screen pixel in the stage's units, never under 1, as the
 * hero's render.ts sets it. The page calls it on load and when its host resizes.
 */
export function linePx(host: HTMLElement, stage: Stage) {
	const width = host.clientWidth;
	if (width) (stage.world as SVGGElement).style.setProperty('--vm-px', String(Math.max(1, Math.round((stage.w / width) * 4) / 4)));
}

/** Tweens a state's numbers, each from its first value to its second. */
export function tween(tl: Timeline, st: object, props: Record<string, [number, number]>, at: number, ms: number, ease: Ease = E.fade) {
	tl.add(st, { ...props, duration: ms, ease }, at);
}

/** A ring's goroutine finishing its job: the check, and the ring's pulse. */
export function done(tl: Timeline, ring: Ring, at: number) {
	tween(tl, ring.st, { c: [0, 1] }, at, 160);
	tween(tl, ring.st, { s: [1, 1.22] }, at, 140, E.out);
	tween(tl, ring.st, { s: [1.22, 1] }, at + 140, 380, E.fade);
	tween(tl, ring.st, { p: [1, 0] }, at + 300, 200);
	tween(tl, ring.st, { c: [1, 0] }, at + 560, 240, E.out);
}

/** Captions on a timeline: each one from its time on. */
export type Captions = [at: number, text: string][];

/**
 * A scene's timeline. It draws the stage on every frame it plays and calls `onTime` with the time in the run, for
 * the caption.
 */
export function timeline(stage: Stage, loop: boolean, hold: number, onTime?: (t: number) => void): Timeline {
	const tl: Timeline = createTimeline({
		autoplay: false,
		loop,
		loopDelay: hold,
		onRender: () => {
			stage.render();
			onTime?.(tl.iterationCurrentTime);
		},
	});
	return tl;
}

/** The caption at time t. */
export const captionAt = (caps: Captions, t: number) => caps.reduce((text, [at, c]) => (t >= at ? c : text), caps[0][1]);
