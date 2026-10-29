'use client';
import { ShikiMagicMovePrecompiled } from '@shikijs/magic-move/react';
import '@shikijs/magic-move/style.css';
import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { STATIC_AT, TOTAL } from '@/lib/heromap/chapters.ts';
import type { CodeSteps } from '@/lib/home/compile.ts';
import { TubeMap, type TubeMapHandle } from './tube-map';

export interface StoryStep {
	/** The step's stretch of the map's loop: where it starts, and how long it plays. */
	from: number;
	ms: number;
	/** When each of its captions starts, in ms from `from`. */
	captions: number[];
	/** The code lines each caption lights, wide and on a phone. */
	lines: { wide: number[][]; phone: number[][] };
	title: ReactNode;
	notes: ReactNode[];
	source: ReactNode;
}

const MORPH = { duration: 700, stagger: 0.02, lineNumbers: false };

function Lit({ lines }: { lines: number[] }) {
	return (
		<div className="hp-lit" aria-hidden="true">
			{lines.map((n) => (
				<i key={n} style={{ '--n': n - 1 } as CSSProperties} />
			))}
		</div>
	);
}

/**
 * The hero and the story on one pinned stage. The page snaps a step at a time: each scroll brings the next step,
 * which plays its chapter on the map once, at the map's own pace, while the one code block morphs into that
 * chapter's code and lights the lines each caption is about. Under reduced motion a step shows its last frame.
 */
export function TubeStory({ hero, steps, code, text }: { hero: ReactNode; steps: StoryStep[]; code: CodeSteps; text: ReactNode }) {
	const root = useRef<HTMLElement>(null);
	const map = useRef<TubeMapHandle>(null);
	const wideCode = useRef<HTMLDivElement>(null);
	const phoneCode = useRef<HTMLDivElement>(null);
	const [step, setStep] = useState(0); // 0 is the hero; k is chapter k
	const [caption, setCaption] = useState(0);
	const current = useRef(0);

	const onTime = useCallback(
		(t: number) => {
			const k = current.current;
			if (k === 0) return;
			const local = (((t - steps[k - 1].from) % TOTAL) + TOTAL) % TOTAL;
			setCaption(Math.max(0, steps[k - 1].captions.findLastIndex((at) => at <= local)));
		},
		[steps],
	);

	useEffect(() => {
		const track = root.current!.querySelector<HTMLElement>('.hp-track')!;
		const reduce = matchMedia('(prefers-reduced-motion: reduce)');
		let frame = 0;
		let shown = -1;
		const tick = () => {
			frame = 0;
			const pad = parseFloat(getComputedStyle(document.documentElement).scrollPaddingTop) || 0;
			const height = (track.firstElementChild as HTMLElement).offsetHeight;
			const k = Math.min(steps.length, Math.max(0, Math.round((pad - track.getBoundingClientRect().top) / height)));
			if (k === shown) return;
			shown = k;
			current.current = k;
			setStep(k);
			setCaption(0);
			if (k === 0) return map.current?.seek(STATIC_AT);
			const s = steps[k - 1];
			if (!reduce.matches) return map.current?.play(s.from, s.ms);
			map.current?.seek(s.from + s.ms);
			setCaption(s.captions.length - 1);
		};
		const schedule = () => {
			frame ||= requestAnimationFrame(tick);
		};
		addEventListener('scroll', schedule, { passive: true });
		addEventListener('resize', schedule);
		tick();
		return () => {
			removeEventListener('scroll', schedule);
			removeEventListener('resize', schedule);
			cancelAnimationFrame(frame);
		};
	}, [steps]);

	// One wheel gesture moves exactly one step: snapping alone lets a short notch fall back to where it was, and a
	// trackpad's momentum run on through several steps. The gesture's later events are absorbed until the wheel goes
	// quiet. Leaving the story at either end, the page scrolls as usual; keyboard and touch scroll natively and snap.
	useEffect(() => {
		const track = root.current!.querySelector<HTMLElement>('.hp-track')!;
		let busy = false;
		let total = 0;
		let quiet = 0;
		const onWheel = (e: WheelEvent) => {
			if (e.ctrlKey || Math.abs(e.deltaY) < Math.abs(e.deltaX)) return;
			const pad = parseFloat(getComputedStyle(document.documentElement).scrollPaddingTop) || 0;
			const height = (track.firstElementChild as HTMLElement).offsetHeight;
			const k = Math.round((pad - track.getBoundingClientRect().top) / height);
			const next = k + Math.sign(e.deltaY);
			if (k < 0 || k > steps.length || next < 0 || next > steps.length) return;
			e.preventDefault();
			clearTimeout(quiet);
			quiet = window.setTimeout(() => {
				busy = false;
				total = 0;
			}, 220);
			if (busy) return;
			total += e.deltaY;
			if (Math.abs(total) < 24) return;
			busy = true;
			const top = track.getBoundingClientRect().top + scrollY - pad + next * height;
			scrollTo({ top, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
		};
		addEventListener('wheel', onWheel, { passive: false });
		return () => {
			removeEventListener('wheel', onWheel);
			clearTimeout(quiet);
		};
	}, [steps.length]);

	const now = step > 0 ? steps[step - 1] : undefined;
	const at = Math.max(0, step - 1);

	// Short of room, the code block shows only some of its lines. Keep the lit ones in view, with the line above them
	// for context when it fits: now, again once the morph has settled at its new height, and when the screen turns.
	useEffect(() => {
		if (!now) return;
		const follow = () => {
			const behavior = matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth';
			for (const [el, lines] of [
				[wideCode.current, now.lines.wide[caption]],
				[phoneCode.current, now.lines.phone[caption]],
			] as const) {
				if (!el?.offsetHeight || !lines?.length) continue;
				const style = getComputedStyle(el);
				const lh = parseFloat(style.getPropertyValue('--hp-lh'));
				const shows = (el.clientHeight - 2 * parseFloat(style.getPropertyValue('--hp-pad'))) / lh;
				const first = Math.min(...lines);
				const context = Math.max(...lines) - first + 1 < shows ? 1 : 0;
				el.scrollTo({ top: (first - 1 - context) * lh, behavior });
			}
		};
		follow();
		const settled = setTimeout(follow, MORPH.duration);
		addEventListener('resize', follow);
		return () => {
			clearTimeout(settled);
			removeEventListener('resize', follow);
		};
	}, [now, caption]);

	return (
		<section ref={root} className="hp-story" aria-label="How a job goes through varmq">
			{/* The whole story as text, for screen readers: the stage only ever shows one step. */}
			<div className="hp-sr">{text}</div>
			<div className="hp-track" aria-hidden="true">
				{Array.from({ length: steps.length + 1 }, (_, i) => (
					<div key={i} className="hp-snap" />
				))}
			</div>
			<div className="hp-stage" data-step={step}>
				<div className="hp-stage-text">
					<div className="hp-hero-panel" data-on={step === 0 || undefined} inert={step !== 0}>
						{hero}
					</div>
					<div className="hp-step-panel" data-on={step > 0 || undefined} inert={step === 0}>
						<div aria-live="polite">
							<p className="hp-count">
								<span>{Math.max(1, step)}</span> / {steps.length}
							</p>
							<h2 className="hp-step-title">{now?.title ?? steps[0].title}</h2>
						</div>
						<ol className="hp-notes-live">
							{(now ?? steps[0]).notes.map((note, i) => (
								<li key={`${step}-${i}`} data-on={(i === caption && step > 0) || undefined}>
									{note}
								</li>
							))}
						</ol>
						<div ref={wideCode} className="hp-morph hp-morph-wide">
							<Lit lines={now?.lines.wide[caption] ?? []} />
							<ShikiMagicMovePrecompiled steps={code.wide} step={at} options={MORPH} />
						</div>
						<div ref={phoneCode} className="hp-morph hp-morph-phone">
							<Lit lines={now?.lines.phone[caption] ?? []} />
							<ShikiMagicMovePrecompiled steps={code.phone} step={at} options={MORPH} />
						</div>
						<p className="hp-source">{(now ?? steps[0]).source}</p>
					</div>
				</div>
				<div className="hp-stage-map">
					<TubeMap ref={map} onTime={onTime} />
				</div>
			</div>
		</section>
	);
}
