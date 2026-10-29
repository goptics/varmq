'use client';
import type { Timeline } from 'animejs';
import { useEffect, useImperativeHandle, useRef, type Ref } from 'react';
import { CHAPTERS, STATIC_AT, TOTAL } from '@/lib/heromap/chapters.ts';
import '@/lib/heromap/map.css';
import { renderMap } from '@/lib/heromap/render.ts';
import { REGIONS, frame } from '@/lib/heromap/scene.ts';

export interface TubeMapHandle {
	/** Shows the map at time `t` of its loop, and holds it there. */
	seek(t: number): void;
	/** Plays the loop from `from` for `ms` at its own pace, then holds at the end. */
	play(from: number, ms: number): void;
}

const wrap = (t: number) => ((t % TOTAL) + TOTAL) % TOTAL;

/**
 * The map: its still frame (the whole network, as at STATIC_AT) until the animation's code has loaded, then whatever
 * the page asks of it. It never plays on its own. `onTime` hears the loop's time on every frame it plays.
 */
export function TubeMap({ ref, onTime }: { ref?: Ref<TubeMapHandle>; onTime?: (t: number) => void }) {
	const host = useRef<HTMLElement>(null);
	const live = useRef<{ timeline?: Timeline; t: number; frame: number; pending?: () => void }>({ t: STATIC_AT, frame: 0 });
	const report = useRef(onTime);
	report.current = onTime;

	useImperativeHandle(ref, () => {
		const stop = () => cancelAnimationFrame(live.current.frame);
		const seek = (t: number) => {
			stop();
			live.current.pending = undefined;
			live.current.t = wrap(t);
			live.current.timeline?.pause().seek(live.current.t);
			report.current?.(live.current.t);
		};
		const play = (from: number, ms: number) => {
			stop();
			const timeline = live.current.timeline;
			if (!timeline) {
				// Not loaded yet: play once it is.
				live.current.t = wrap(from);
				live.current.pending = () => play(from, ms);
				return;
			}
			live.current.pending = undefined;
			timeline.seek(wrap(from)).play();
			const begun = performance.now();
			const tick = () => {
				if (performance.now() - begun >= ms) {
					live.current.t = wrap(from + ms);
					timeline.pause().seek(live.current.t);
					report.current?.(live.current.t);
					return;
				}
				report.current?.(timeline.iterationCurrentTime);
				live.current.frame = requestAnimationFrame(tick);
			};
			live.current.frame = requestAnimationFrame(tick);
		};
		return { seek, play };
	}, []);

	useEffect(() => {
		const el = host.current!;
		const nodes = renderMap(el);
		nodes.setCamera(frame(REGIONS[REGIONS.length - 1]));
		for (const word of nodes.words(CHAPTERS.length, 0)) word.style.opacity = '1';
		const resize = new ResizeObserver(([entry]) => nodes.setWidth(entry.contentRect.width));
		resize.observe(el);

		let gone = false;
		import('@/lib/heromap/choreo.ts').then(({ buildTimeline }) => {
			if (gone) return;
			const timeline = buildTimeline(nodes).pause();
			timeline.seek(live.current.t);
			live.current.timeline = timeline;
			live.current.pending?.();
		});
		return () => {
			gone = true;
			cancelAnimationFrame(live.current.frame);
			resize.disconnect();
			live.current.timeline?.pause();
			live.current.timeline = undefined;
			el.replaceChildren();
		};
	}, []);

	return <varmq-map ref={host} aria-hidden="true" />;
}
