'use client';
import type { Timeline } from 'animejs';
import { useEffect, useImperativeHandle, useRef, type Ref } from 'react';
import { CHAPTERS, STATIC_AT, TOTAL } from '@/lib/heromap/chapters.ts';
import '@/lib/heromap/map.css';
import { renderMap } from '@/lib/heromap/render.ts';
import { REGIONS, frame } from '@/lib/heromap/scene.ts';

export interface TubeMapHandle {
	/** Shows the map at time `t` of its loop. */
	seek(t: number): void;
}

/**
 * The map: its still frame (the whole network, as at STATIC_AT) until the animation's code has loaded, then whatever
 * moment the page seeks. It never plays on its own; the page's scroll moves it.
 */
export function TubeMap({ ref }: { ref?: Ref<TubeMapHandle> }) {
	const host = useRef<HTMLElement>(null);
	const live = useRef<{ timeline?: Timeline; t: number }>({ t: STATIC_AT });

	useImperativeHandle(
		ref,
		() => ({
			seek(t) {
				const at = ((t % TOTAL) + TOTAL) % TOTAL;
				if (at === live.current.t) return;
				live.current.t = at;
				live.current.timeline?.seek(at);
			},
		}),
		[],
	);

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
		});
		return () => {
			gone = true;
			resize.disconnect();
			live.current.timeline?.pause();
			live.current.timeline = undefined;
			el.replaceChildren();
		};
	}, []);

	return <varmq-map ref={host} role="img" aria-label="The map of the story: each step below runs on it as you scroll." />;
}
