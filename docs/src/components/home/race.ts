// Benchmark lanes: bars and counters count up once when a lane list is first seen, and again on Replay.

import { reducedMotion } from '../ui/dom.ts';

const root = document.querySelector<HTMLElement>('[data-race]');

function format(v: number, dec: number, suffix: string) {
	return v.toLocaleString('en-US', { minimumFractionDigits: dec, maximumFractionDigits: dec }) + suffix;
}

/** The latest count-up of each value: an earlier one, still running when Replay starts another, stops writing. */
const runs = new WeakMap<HTMLElement, number>();
let run = 0;

function play(list: HTMLElement) {
	if (reducedMotion()) return;
	const total = 1100;
	const mine = ++run;
	list.querySelectorAll<HTMLElement>('.rlane').forEach((lane, i) => {
		const bar = lane.querySelector<HTMLElement>('.rlane__bar');
		const val = lane.querySelector<HTMLElement>('.rlane__val');
		if (!bar || !val) return;
		const target = parseFloat(bar.style.getPropertyValue('--s') || '1');
		const v = parseFloat(val.dataset.v ?? '0');
		const dec = parseInt(val.dataset.dec ?? '0', 10);
		const suffix = val.dataset.suffix ?? '';
		const delay = i * 60;
		for (const a of bar.getAnimations()) a.cancel();
		runs.set(val, mine);
		bar.animate([{ transform: 'scaleX(0)' }, { transform: `scaleX(${target})` }], {
			duration: total,
			delay,
			easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)',
			fill: 'backwards',
		});
		const t0 = performance.now() + delay;
		const tick = (now: number) => {
			if (runs.get(val) !== mine) return;
			const p = Math.max(0, Math.min(1, (now - t0) / total));
			const eased = 1 - Math.pow(1 - p, 3);
			val.textContent = format(v * eased, dec, suffix);
			if (p < 1) requestAnimationFrame(tick);
			else val.textContent = format(v, dec, suffix);
		};
		requestAnimationFrame(tick);
	});
}

if (root) {
	const lists = Array.from(root.querySelectorAll<HTMLElement>('.rlanes'));
	const seen = new WeakSet<Element>();

	// A list inside a hidden tab panel reports as not intersecting until its tab opens.
	const io = new IntersectionObserver(
		(entries) => {
			for (const e of entries) {
				if (e.isIntersecting && !seen.has(e.target)) {
					seen.add(e.target);
					play(e.target as HTMLElement);
				}
			}
		},
		{ threshold: 0.35 },
	);
	lists.forEach((l) => io.observe(l));

	root.querySelector('[data-replay]')?.addEventListener('click', () => {
		for (const l of lists) if (l.offsetParent !== null) play(l);
	});
}
