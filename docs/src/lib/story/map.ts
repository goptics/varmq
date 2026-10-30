import type { Timeline } from 'animejs';
import { STATIC_AT, TOTAL } from '../heromap/chapters.ts';
import { renderMap } from '../heromap/render.ts';
import { REGIONS, frame } from '../heromap/scene.ts';

/** A stretch that plays over: it holds its end `again` ms, then plays again from `restart` (its start by default) to
 * the same end. */
export interface Loop {
	again: number;
	restart?: number;
}

export interface MapHandle {
	/** Shows the map at time `t` of its loop, and holds it there. */
	seek(t: number): void;
	/**
	 * Plays the loop from `from` for `ms` at its own pace, then holds at the end; given `loop`, plays it over until told
	 * otherwise. `onTime` hears only its first run, so what follows the map's clock stays as that run left it.
	 */
	play(from: number, ms: number, loop?: Loop): void;
	/**
	 * Plays the loop backward from where the map is to `to`, over `most` ms of it at most: from further on, it starts
	 * `most` ms past `to`, and it may go back over the loop's end. It takes REWIND ms at most, faster than the loop's own
	 * pace if need be. Then it holds at `to`, and calls `then` if given.
	 */
	rewind(to: number, most: number, then?: () => void): void;
	/** The loop's time the map shows. */
	now(): number;
}

const wrap = (t: number) => ((t % TOTAL) + TOTAL) % TOTAL;
/** The longest the map's timeline builds for at a time, in ms, before it lets the page go on. */
const BUILD = 8;
/**
 * Runs `then` once the page is idle, or after 50 ms at the latest; where there is no requestIdleCallback (Safari), in
 * a task of its own.
 */
const later = (then: () => void) =>
	typeof requestIdleCallback === 'function' ? requestIdleCallback(then, { timeout: 50 }) : setTimeout(then);
/** The longest a rewind takes, in ms: long enough to see what goes back, short enough to keep up with a scroll. */
const REWIND = 1000;

/**
 * Shows a timeline at times after `end` in any order, as a full seek would, but cheaply. A full seek redraws the loop
 * from 0, which takes up to 20 ms late in it, and anime.js going backward by itself keeps stale values over this
 * score's tweens (score.ts). So it seeks to `end` once, in full; each time after that is anime.js's own seek forward
 * from `end`, which it renders right, once every attribute the time before changed under `host` is put back, and
 * `state`, an object the timeline tweens outside the page (the map's camera), is as it was at `end`.
 */
export function after(tl: Timeline, host: Element, end: number, state: object = {}) {
	const seek = Object.getPrototypeOf(tl).seek as Timeline['seek'];
	tl.pause().seek(end);
	const atEnd = { ...state };
	const clean = new Map<Element, Map<string, string | null>>();
	const watch = new MutationObserver(() => {});
	watch.observe(host, { attributes: true, subtree: true, attributeOldValue: true });
	const note = () => {
		for (const r of watch.takeRecords()) {
			const attrs = clean.get(r.target as Element) ?? new Map<string, string | null>();
			clean.set(r.target as Element, attrs);
			if (!attrs.has(r.attributeName!)) attrs.set(r.attributeName!, r.oldValue);
		}
	};
	return {
		/** Shows time `at`, no earlier than `end` and before the loop's end. */
		show(at: number) {
			seek.call(tl, end);
			note();
			for (const [el, attrs] of clean) {
				for (const [name, value] of attrs) {
					if (value === null) el.removeAttribute(name);
					else el.setAttribute(name, value);
				}
			}
			watch.takeRecords();
			Object.assign(state, atEnd);
			seek.call(tl, at);
			note();
		},
		stop: () => watch.disconnect(),
	};
}

/**
 * Draws the map into `host`: its still frame (the whole network, as at STATIC_AT) until the animation's code has
 * loaded, then whatever the page asks of it. It never plays on its own. `onTime` hears the loop's time on every frame
 * it plays.
 */
export function mountMap(host: HTMLElement, onTime: (t: number) => void): MapHandle {
	const nodes = renderMap(host);
	nodes.setCamera(frame(REGIONS[REGIONS.length - 1]));
	new ResizeObserver(([entry]) => nodes.setWidth(entry.contentRect.width)).observe(host);

	let timeline: Timeline | undefined;
	let t = STATIC_AT;
	let frameId = 0;
	let pending: (() => void) | undefined;
	let frames: ReturnType<typeof after> | undefined;
	let camera: object | undefined;

	const cancel = () => {
		cancelAnimationFrame(frameId);
		frames?.stop();
		frames = undefined;
	};

	const seek = (at: number) => {
		cancel();
		pending = undefined;
		t = wrap(at);
		timeline?.pause().seek(t);
		onTime(t);
	};

	// A play or a rewind stops a frame or two short of its end, where the map is already still, rather than a frame past
	// it: past the end the next stretch has begun (the finale's fade, say), and the seek back from there flickers. Two
	// frames, as the timeline's clock can run a frame ahead of this one.
	const ending = (now: number, last: number, begun: number, ms: number) => now + 2 * (now - last) - begun >= ms;

	const play = (from: number, ms: number, loop?: Loop) => {
		cancel();
		const tl = timeline;
		if (!tl) {
			// Not loaded yet: play once it is.
			t = wrap(from);
			pending = () => play(from, ms, loop);
			return;
		}
		pending = undefined;
		const end = from + ms;
		let start = from;
		let first = true;
		let begun = performance.now();
		tl.seek(wrap(from)).play();
		let last = performance.now();
		const tick = () => {
			const now = performance.now();
			if (!tl.paused && ending(now, last, begun, end - start)) {
				t = wrap(end);
				tl.pause().seek(t);
				if (first) onTime(t);
				if (!loop) return;
			} else if (tl.paused && now - begun >= end - start + loop!.again) {
				start = loop!.restart ?? from;
				first = false;
				begun = now;
				tl.seek(wrap(start)).play();
			} else if (!tl.paused) {
				t = tl.iterationCurrentTime;
				if (first) onTime(t);
			}
			last = now;
			frameId = requestAnimationFrame(tick);
		};
		frameId = requestAnimationFrame(tick);
	};

	const rewind = (to: number, most: number, then?: () => void) => {
		cancel();
		const tl = timeline;
		if (!tl) {
			t = wrap(to);
			pending = () => {
				seek(to);
				then?.();
			};
			return;
		}
		pending = undefined;
		const ms = Math.min(wrap(t - to), most);
		const end = wrap(to);
		onTime(end);
		// `after` shows only times past its own end, so a rewind back over the loop's end goes in two parts: down to the
		// loop's start, then from the loop's end, which shows as its start does, down to `to`.
		let from = -1;
		const show = (left: number) => {
			const u = end + Math.max(0, left); // past TOTAL while it is still on the far side of the loop's end
			const base = u >= TOTAL ? 0 : end;
			if (base !== from) {
				frames?.stop();
				frames = after(tl, host, base, camera);
				from = base;
			}
			t = Math.min(u - (base === 0 ? TOTAL : 0), TOTAL - 0.001);
			frames!.show(t);
		};
		show(ms);
		const begun = performance.now();
		const pace = Math.max(1, ms / REWIND);
		const tick = () => {
			const left = ms - (performance.now() - begun) * pace;
			show(left);
			if (left > 0) {
				frameId = requestAnimationFrame(tick);
				return;
			}
			cancel();
			// The frame is exact already; a full seek leaves anime.js's own state, and the renderer's, exact too.
			tl.seek(t);
			onTime(t);
			then?.();
		};
		frameId = requestAnimationFrame(tick);
	};

	// Building the timeline takes a few hundred ms on a slow phone, so it goes in steps: it lets the page scroll and draw
	// once BUILD ms have passed, and again before its first seek and before what was asked of it meanwhile, each a full
	// seek. The still frame stays until it is done.
	import('../heromap/choreo.ts').then(({ Choreo, browserPage }) => {
		const choreo = new Choreo(nodes, browserPage(nodes.world));
		camera = choreo.cam;
		const steps = choreo.building();
		const next = () => {
			const begun = performance.now();
			let step = steps.next();
			while (!step.done && performance.now() - begun < BUILD) step = steps.next();
			if (!step.done) {
				later(next);
				return;
			}
			const built = step.value.pause();
			later(() => {
				timeline = built;
				timeline.seek(t);
				later(() => pending?.());
			});
		};
		later(next);
	});
	return { seek, play, rewind, now: () => t };
}
