'use client';
import { useEffect, useRef, type CSSProperties, type ReactNode } from 'react';
import { STATIC_AT, TOTAL } from '@/lib/heromap/chapters.ts';
import { TubeMap, type TubeMapHandle } from './tube-map';

export interface RunStation {
	/** The station's stretch of the map's loop, in ms. */
	start: number;
	end: number;
	/** Its captions' windows, in ms from `start`. */
	steps: { at: number; until: number }[];
	content: ReactNode;
}

const clamp01 = (x: number) => Math.min(1, Math.max(0, x));

/**
 * The hero and the story beside one map. Scrolling the hero plays the map from its still frame into station 1;
 * after that, the station whose text holds at the reading line plays its own stretch, in proportion to how far the
 * page has scrolled through it. Under reduced motion the map jumps to the middle of each step instead.
 */
export function TubeRun({ hero, stations }: { hero: ReactNode; stations: RunStation[] }) {
	const root = useRef<HTMLDivElement>(null);
	const line = useRef<HTMLDivElement>(null);
	const map = useRef<TubeMapHandle>(null);

	useEffect(() => {
		const blocks = [...root.current!.querySelectorAll<HTMLElement>('[data-station]')];
		const reduce = matchMedia('(prefers-reduced-motion: reduce)');
		let frame = 0;
		let shown = '';

		const tick = () => {
			frame = 0;
			const read = line.current!.getBoundingClientRect().top;
			const first = blocks[0].getBoundingClientRect();
			let t: number;
			let station = -1;
			let step = 0;
			if (blocks.at(-1)!.getBoundingClientRect().bottom <= read) {
				t = stations.at(-1)!.end; // past the story: the map holds its last frame, and no station is lit
			} else if (first.top > read) {
				const span = first.top + scrollY - read;
				t = reduce.matches ? STATIC_AT : STATIC_AT + clamp01(span > 0 ? scrollY / span : 1) * (TOTAL - STATIC_AT);
			} else {
				station = blocks.findLastIndex((b) => b.getBoundingClientRect().top <= read);
				const box = blocks[station].getBoundingClientRect();
				// The stretch is the scroll its text stays held for: the block less the text.
				const held = Math.max(1, box.height - (blocks[station].firstElementChild as HTMLElement).offsetHeight);
				const { start, end, steps } = stations[station];
				const local = clamp01((read - box.top) / held) * (end - start);
				step = Math.max(0, steps.findLastIndex((s) => s.at <= local));
				t = reduce.matches ? start + (steps[step].at + steps[step].until) / 2 : start + local;
			}
			map.current?.seek(t);

			const now = `${station}:${step}`;
			if (now === shown) return;
			shown = now;
			blocks.forEach((b, i) => {
				b.toggleAttribute('data-on', i === station);
				b.dataset.step = String(i === station ? step : -1);
			});
		};
		const schedule = () => {
			frame ||= requestAnimationFrame(tick);
		};

		addEventListener('scroll', schedule, { passive: true });
		addEventListener('resize', schedule);
		reduce.addEventListener('change', schedule);
		tick();
		return () => {
			removeEventListener('scroll', schedule);
			removeEventListener('resize', schedule);
			reduce.removeEventListener('change', schedule);
			cancelAnimationFrame(frame);
		};
	}, [stations]);

	return (
		<div ref={root} className="hp-run">
			<div ref={line} className="hp-readline" aria-hidden="true" />
			<div className="hp-run-hero">{hero}</div>
			<div className="hp-run-map">
				<div className="hp-run-pin">
					<TubeMap ref={map} />
				</div>
			</div>
			<ol className="hp-run-stations">
				{stations.map((s, i) => (
					<li
						key={i}
						data-station={i}
						data-step={-1}
						className="hp-station"
						style={{ '--hp-dur': (s.end - s.start) / 1000 } as CSSProperties}
					>
						<div className="hp-station-body">{s.content}</div>
					</li>
				))}
			</ol>
		</div>
	);
}
