// The hero map's score: every animated property's track, and the cues the choreographer (`choreo.ts`) writes,
// which become one anime.js timeline. A cue is a tween or a set at a time; a track is what one property of one
// element (or of the camera) does over the loop.
//
// What the timeline needs, and why the score is kept this way:
// - Every tween names its from-value, the property's value at its start, even while an earlier tween still runs:
//   anime.js cuts that one short there, so nothing jumps. The timeline then needs no composition pass of its own,
//   which would render the whole timeline again for each of the thousands of tweens added.
// - Cues are added in time order, so the tweens on one property are siblings in the order they play. anime.js
//   renders them right in playback. It does not when the playhead jumps back, or over the loop's end: a tween with
//   its from-value written out does not take back the stretch before it, and the stale value of a later one
//   stays. So every property is set at 0, and `seek` draws a frame from 0 (see `build`).
// - A set lasts 1e-11 ms. In playback the playhead jumps past one in a single frame, and anime.js applies it only
//   while it is not yet past the time the next tween on its property takes over. With no next tween, that test
//   is a float comparison that some loads fail (it depends on the timeline's offset). So no set ends its
//   property's track: `finish` follows each such set with a few ms of tween to the same value.

import { createTimeline, stagger, type AnimationParams, type Timeline } from 'animejs';

export type Pt = { x: number; y: number };
export type Ease = (t: number) => number;
export type Values = Record<string, number | number[]>;

/** What the score needs from the page: motion paths and drawable outlines (anime.js's `svg` makes the outlines). */
export interface Stage {
	/** Tween values that move a square along a polyline: `translateX` and `translateY`. */
	path(pts: Pt[]): { translateX: unknown; translateY: unknown };
	/** An outline's drawable stand-in, whose `draw` anime.js tweens. */
	drawable(el: Element): object;
}

export const pt = (x: number, y: number): Pt => ({ x, y });
export const dist = (a: Pt, b: Pt) => Math.hypot(b.x - a.x, b.y - a.y);
export const lengthOf = (pts: Pt[]) => pts.slice(1).reduce((n, p, i) => n + dist(pts[i], p), 0);
export const clamp01 = (t: number) => Math.min(1, Math.max(0, t));
const lerp = (a: number[], b: number[], f: number) => a.map((x, i) => x + (b[i] - x) * f);
const linear: Ease = (t) => t;

/** The point `d` along a polyline, as an SVG path's getPointAtLength gives it. */
export function along(pts: Pt[], d: number): Pt {
	for (let i = 1; i < pts.length; i++) {
		const seg = dist(pts[i - 1], pts[i]);
		if (d <= seg || i === pts.length - 1) {
			const f = seg === 0 ? 1 : clamp01(d / seg);
			return pt(pts[i - 1].x + (pts[i].x - pts[i - 1].x) * f, pts[i - 1].y + (pts[i].y - pts[i - 1].y) * f);
		}
		d -= seg;
	}
	return pts[0];
}

/** Where a square moving along `pts` from `start` over `duration` is at t: at the ends before and after. */
export function trajectory(pts: Pt[], start: number, duration: number, ease: Ease): (t: number) => Pt {
	const length = lengthOf(pts);
	return (t) => along(pts, ease(clamp01((t - start) / duration)) * length);
}

/** The progress (0 to 1) at which `ease` reaches `value`: eases rise, so a bisection finds it. */
export function solve(ease: Ease, value: number): number {
	let lo = 0;
	let hi = 1;
	for (let i = 0; i < 30; i++) {
		const mid = (lo + hi) / 2;
		if (ease(mid) < value) lo = mid;
		else hi = mid;
	}
	return hi;
}

/** One stretch of a property's change: from its start to its end, and its value in between. A set has no length. */
export interface Seg {
	start: number;
	end: number;
	at(t: number): number[];
}

/** A property's value over the loop: where it starts, and the tweens and sets that change it, in the order they start. */
export class Track {
	readonly name: string;
	readonly initial: number[];
	readonly segs: Seg[] = [];

	constructor(name: string, initial: number[]) {
		this.name = name;
		this.initial = initial;
	}

	/** The value at t: the latest tween or set started by then, held at its end once it is over. */
	at(t: number): number[] {
		let value = this.initial;
		for (const s of this.segs) {
			if (s.start > t) break;
			value = s.at(Math.min(t, s.end));
		}
		return value;
	}

	/** When the latest tween ends. */
	get end(): number {
		const last = this.segs[this.segs.length - 1];
		return last ? last.end : 0;
	}

	/** Whether the track ends in a set: its start at 0, or a set after its last tween. */
	get endsInSet(): boolean {
		const last = this.segs[this.segs.length - 1];
		return !last || last.start === last.end;
	}

	push(seg: Seg) {
		const last = this.segs[this.segs.length - 1];
		if (last && seg.start < last.start) {
			throw new Error(`heromap: ${this.name} tweened at ${seg.start} ms, before its tween at ${last.start} ms`);
		}
		this.segs.push(seg);
	}
}

export interface Cue {
	at: number;
	targets: object;
	params: AnimationParams;
	set?: boolean;
	stagger?: number; // a cue on several targets, each this many ms after the one before
}

/** A set happens this long before its time, so a frame at exactly its time has it done (see `Score.set`). */
export const SET_EARLY = 0.5;
/** The tween that follows a set that would end its track: this long after it, and this long. */
export const PAD = { after: 1, duration: 4 };

/**
 * How many cues `building` adds between its stops. Each add takes longer as the timeline grows (anime.js walks every
 * tween already in it), up to about 0.2 ms on a slow phone: a step stays under 5 ms.
 */
const SLICE = 25;

/** Runs `steps` to its end, at once, and returns what it returns. */
export function drain<T>(steps: Generator<void, T>): T {
	for (;;) {
		const step = steps.next();
		if (step.done) return step.value;
	}
}

const asArray = (v: number | number[]) => (typeof v === 'number' ? [v] : v);
const format = (prop: string, v: number[]) => (prop === 'draw' ? `${v[0]} ${v[1]}` : v[0]);
/** A track's anime.js properties: `pos` is two, translateX and translateY. */
const props = (track: Track, v: number[]): [string, string | number][] =>
	track.name === 'pos'
		? [
				['translateX', v[0]],
				['translateY', v[1]],
			]
		: [[track.name, format(track.name, v)]];

export class Score {
	readonly cues: Cue[] = [];
	/** How long the loop is: no cue may run past it. */
	private readonly total: number;
	private readonly stage: Stage;
	private readonly tracks = new Map<object, Map<string, Track>>();
	private readonly drawables = new Map<Element, object>();
	private finished = false;

	constructor(total: number, stage: Stage) {
		this.total = total;
		this.stage = stage;
	}

	/** The one drawable stand-in of an outline, so all its `draw` tweens are siblings. */
	drawable(el: Element): object {
		let proxy = this.drawables.get(el);
		if (!proxy) {
			proxy = this.stage.drawable(el);
			this.drawables.set(el, proxy);
		}
		return proxy;
	}

	/** Declares where properties start, each loop: set at 0. A property declared again keeps its first start. */
	start(target: object, values: Values) {
		let tracks = this.tracks.get(target);
		if (!tracks) this.tracks.set(target, (tracks = new Map()));
		for (const [prop, value] of Object.entries(values)) {
			if (!tracks.has(prop)) tracks.set(prop, new Track(prop, asArray(value)));
		}
	}

	track(target: object, prop: string): Track {
		const track = this.tracks.get(target)?.get(prop);
		if (!track) throw new Error(`heromap: ${prop} has no start`);
		return track;
	}

	/** Every track, with its target. */
	*all(): Generator<[object, Track]> {
		for (const [target, tracks] of this.tracks) for (const track of tracks.values()) yield [target, track];
	}

	at(target: object, prop: string, t: number): number[] {
		return this.track(target, prop).at(t);
	}

	/** Tweens `values` from where they are at `start` (or from `from`), over `duration`. */
	tween(target: object, values: Values, start: number, duration: number, ease: Ease, from: Values = {}) {
		const params: AnimationParams = { duration, ease };
		for (const [prop, value] of Object.entries(values)) {
			const track = this.track(target, prop);
			const a = prop in from ? asArray(from[prop]) : track.at(start);
			const b = asArray(value);
			track.push({ start, end: start + duration, at: (t) => lerp(a, b, ease(clamp01((t - start) / duration))) });
			params[prop] = [format(prop, a), format(prop, b)];
		}
		this.cues.push({ at: start, targets: target, params });
	}

	/** Tweens each of `targets` as `tween` does, each `gap` ms after the one before: one cue, staggered by anime.js. */
	stagger(targets: object[], values: Values, start: number, gap: number, duration: number, ease: Ease) {
		const params: AnimationParams = { duration, ease };
		for (const [prop, value] of Object.entries(values)) {
			const b = asArray(value);
			const froms = targets.map((target, k) => {
				const s = start + gap * k;
				const track = this.track(target, prop);
				const a = track.at(s);
				track.push({ start: s, end: s + duration, at: (t) => lerp(a, b, ease(clamp01((t - s) / duration))) });
				return format(prop, a);
			});
			if (froms.some((f) => f !== froms[0])) throw new Error(`heromap: staggered targets differ in ${prop}`);
			params[prop] = [froms[0], format(prop, b)];
		}
		this.cues.push({ at: start, targets, params, stagger: gap });
	}

	/**
	 * Sets `values` at once, by `at`. A set lasts 1e-11 ms in anime.js, under float precision this far into the
	 * timeline, so a frame at exactly its time could show it half done: it happens SET_EARLY ms early.
	 */
	set(target: object, values: Values, at: number) {
		const t = at - SET_EARLY;
		const params: AnimationParams = {};
		for (const [prop, value] of Object.entries(values)) {
			const b = asArray(value);
			this.track(target, prop).push({ start: t, end: t, at: () => b });
			params[prop] = format(prop, b);
		}
		this.cues.push({ at: t, targets: target, params, set: true });
	}

	/** Moves `el` from where it is at `start` along `route`, over `duration`. */
	move(el: Element, route: Pt[], start: number, duration: number, ease: Ease) {
		const track = this.track(el, 'pos');
		const [x, y] = track.at(start);
		const pts = [pt(x, y), ...route].filter((p, i, all) => i === 0 || dist(all[i - 1], p) > 0.01);
		if (pts.length < 2) return;
		const path = trajectory(pts, start, duration, ease);
		track.push({
			start,
			end: start + duration,
			at: (t) => {
				const p = path(t);
				return [p.x, p.y];
			},
		});
		const [a, b] = [pts[0], pts[pts.length - 1]];
		const motion =
			pts.length === 2 ? { translateX: [a.x, b.x], translateY: [a.y, b.y] } : this.stage.path(pts);
		this.cues.push({ at: start, targets: el, params: { ...motion, duration, ease } as AnimationParams });
	}

	/** Puts `el` at `p` at once, by `at` (see `set`). */
	place(el: Element, p: Pt, at: number) {
		const t = at - SET_EARLY;
		this.track(el, 'pos').push({ start: t, end: t, at: () => [p.x, p.y] });
		this.cues.push({ at: t, targets: el, params: { translateX: p.x, translateY: p.y }, set: true });
	}

	/** Where `el` is at t. */
	pos(el: Element, t: number): Pt {
		const [x, y] = this.at(el, 'pos', t);
		return pt(x, y);
	}

	/**
	 * Ends no track in a set: each one that would (its start at 0 included) gets PAD.duration ms of tween to the
	 * same value, PAD.after ms after it. Nothing comes after it on its track, so the pad overlaps nothing. Called
	 * once, by `build`; a test calls it to see the score whole.
	 */
	finish() {
		if (this.finished) return;
		this.finished = true;
		const pads = new Map<string, { at: number; prop: string; value: string | number; targets: object[] }>();
		for (const [target, track] of this.all()) {
			if (!track.endsInSet) continue;
			const last = track.segs[track.segs.length - 1];
			const at = (last ? last.end : 0) + PAD.after;
			const value = track.at(at);
			if (at + PAD.duration > this.total) throw new Error(`heromap: no room after ${track.name}'s last set at ${at} ms`);
			track.push({ start: at, end: at + PAD.duration, at: () => value });
			for (const [prop, v] of props(track, value)) {
				const key = `${at}|${prop}|${v}`;
				const pad = pads.get(key) ?? { at, prop, value: v, targets: [] };
				pad.targets.push(target);
				pads.set(key, pad);
			}
		}
		for (const { at, prop, value, targets } of pads.values()) {
			this.cues.push({ at, targets, params: { [prop]: [value, value], duration: PAD.duration, ease: linear } });
		}
	}

	/** The cues in the order they go into the timeline: by time, a set before a tween at one instant. */
	ordered(): Cue[] {
		const cues = this.cues.map((cue, i) => ({ cue, i }));
		cues.sort((a, b) => a.cue.at - b.cue.at || Number(!!b.cue.set) - Number(!!a.cue.set) || a.i - b.i);
		return cues.map(({ cue }) => cue);
	}

	/** The timeline: every start set at 0, then every cue in time order. */
	build(onRender: () => void): Timeline {
		return drain(this.building(onRender));
	}

	/**
	 * `build` in steps: it stops after each start it sets and every SLICE cues it adds, so a page can add them between
	 * frames. Adding renders nothing (the timeline has no composition), so the timeline is the same however the steps
	 * are spread.
	 */
	*building(onRender: () => void): Generator<void, Timeline> {
		this.finish();
		const timeline = createTimeline({ loop: true, autoplay: false, composition: false, onRender });
		// One set per start value, over all the targets that share it.
		const resets = new Map<string, { prop: string; value: string | number; targets: object[] }>();
		for (const [target, track] of this.all()) {
			for (const [prop, value] of props(track, track.initial)) {
				const key = `${prop}=${value}`;
				const reset = resets.get(key) ?? { prop, value, targets: [] };
				reset.targets.push(target);
				resets.set(key, reset);
			}
		}
		for (const { prop, value, targets } of resets.values()) {
			timeline.set(targets, { [prop]: value }, 0);
			yield;
		}
		// Every frame sought is drawn from 0, where every property is set (see the header).
		const seek = timeline.seek.bind(timeline);
		timeline.seek = (time, muteCallbacks, internalRender) => {
			seek(0, muteCallbacks, internalRender);
			return seek(time, muteCallbacks, internalRender);
		};
		let added = 0;
		for (const cue of this.ordered()) {
			if (++added % SLICE === 0) yield;
			const lag = cue.stagger === undefined ? 0 : cue.stagger * ((cue.targets as object[]).length - 1);
			if (cue.at + lag + Number(cue.params.duration ?? 0) > this.total + SET_EARLY) {
				throw new Error(`heromap: a tween at ${cue.at} ms runs past the loop's end`);
			}
			if (cue.stagger !== undefined) timeline.add(cue.targets, cue.params, stagger(cue.stagger, { start: cue.at }));
			else if (cue.set) timeline.set(cue.targets, cue.params, cue.at);
			else timeline.add(cue.targets, cue.params, cue.at);
		}
		return timeline;
	}
}
