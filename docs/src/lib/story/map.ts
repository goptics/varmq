import type { Timeline } from 'animejs';
import { CHAPTERS, STATIC_AT, TOTAL } from '../heromap/chapters.ts';
import { renderMap } from '../heromap/render.ts';
import { REGIONS, frame } from '../heromap/scene.ts';

export interface MapHandle {
	/** Shows the map at time `t` of its loop, and holds it there. */
	seek(t: number): void;
	/**
	 * Plays the loop from `from` for `ms` at its own pace, then holds at the end. Given `again`, it holds the end that
	 * many ms and plays the stretch over, until told otherwise.
	 */
	play(from: number, ms: number, again?: number): void;
}

const wrap = (t: number) => ((t % TOTAL) + TOTAL) % TOTAL;

/**
 * Draws the map into `host`: its still frame (the whole network, as at STATIC_AT) until the animation's code has
 * loaded, then whatever the page asks of it. It never plays on its own. `onTime` hears the loop's time on every frame
 * it plays.
 */
export function mountMap(host: HTMLElement, onTime: (t: number) => void): MapHandle {
	const nodes = renderMap(host);
	nodes.setCamera(frame(REGIONS[REGIONS.length - 1]));
	for (const word of nodes.words(CHAPTERS.length, 0)) word.style.opacity = '1';
	new ResizeObserver(([entry]) => nodes.setWidth(entry.contentRect.width)).observe(host);

	let timeline: Timeline | undefined;
	let t = STATIC_AT;
	let frameId = 0;
	let pending: (() => void) | undefined;

	const seek = (at: number) => {
		cancelAnimationFrame(frameId);
		pending = undefined;
		t = wrap(at);
		timeline?.pause().seek(t);
		onTime(t);
	};

	const play = (from: number, ms: number, again?: number) => {
		cancelAnimationFrame(frameId);
		const tl = timeline;
		if (!tl) {
			// Not loaded yet: play once it is.
			t = wrap(from);
			pending = () => play(from, ms, again);
			return;
		}
		pending = undefined;
		tl.seek(wrap(from)).play();
		let begun = performance.now();
		let last = begun;
		const tick = () => {
			const now = performance.now();
			// It stops a frame or two short of the end, where every step's map is already still, rather than a frame past
			// it: past the end the next stretch has begun (the finale's fade, say), and the seek back from there flickers.
			// Two frames, as the timeline's clock can run a frame ahead of this one.
			const ending = now + 2 * (now - last) - begun >= ms;
			last = now;
			if (!tl.paused && ending) {
				t = wrap(from + ms);
				tl.pause().seek(t);
				onTime(t);
				if (again === undefined) return;
			} else if (tl.paused && now - begun >= ms + again!) {
				begun = last = now;
				tl.seek(wrap(from)).play();
			} else if (!tl.paused) onTime(tl.iterationCurrentTime);
			frameId = requestAnimationFrame(tick);
		};
		frameId = requestAnimationFrame(tick);
	};

	import('../heromap/choreo.ts').then(({ buildTimeline }) => {
		timeline = buildTimeline(nodes).pause();
		timeline.seek(t);
		pending?.();
	});
	return { seek, play };
}
