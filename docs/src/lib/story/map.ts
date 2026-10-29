import type { Timeline } from 'animejs';
import { CHAPTERS, STATIC_AT, TOTAL } from '../heromap/chapters.ts';
import { renderMap } from '../heromap/render.ts';
import { REGIONS, frame } from '../heromap/scene.ts';

export interface MapHandle {
	/** Shows the map at time `t` of its loop, and holds it there. */
	seek(t: number): void;
	/** Plays the loop from `from` for `ms` at its own pace, then holds at the end. */
	play(from: number, ms: number): void;
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

	const play = (from: number, ms: number) => {
		cancelAnimationFrame(frameId);
		const tl = timeline;
		if (!tl) {
			// Not loaded yet: play once it is.
			t = wrap(from);
			pending = () => play(from, ms);
			return;
		}
		pending = undefined;
		tl.seek(wrap(from)).play();
		const begun = performance.now();
		const tick = () => {
			if (performance.now() - begun >= ms) {
				t = wrap(from + ms);
				tl.pause().seek(t);
				onTime(t);
				return;
			}
			onTime(tl.iterationCurrentTime);
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
