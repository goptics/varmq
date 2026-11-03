// Behavior that belongs to the guide scenes: a frame clock with a pause button, a polite live region
// that does not repeat itself, the line that shows the last Go call, and small DOM helpers.
// Motion preference, the element builder, tabs and timers come from `../ui/dom`.

import { reducedMotion, setCall } from '../ui/dom';

export const qs = <T extends HTMLElement>(root: ParentNode, sel: string) => root.querySelector<T>(sel)!;
export const qsa = <T extends HTMLElement>(root: ParentNode, sel: string) => Array.from(root.querySelectorAll<T>(sel));

/** Seconds with one decimal: 2400 -> "2.4 s". */
export const sec = (ms: number) => `${(ms / 1000).toFixed(1)} s`;

/**
 * With reduced motion a press shows its end state: `advance` moves the model 100 ms at a time until
 * `moving` says nothing more happens on its own, for at most `limit` ms of model time.
 */
export function settle(advance: (ms: number) => void, moving: () => boolean, limit = 120_000) {
	for (let t = 0; t < limit && moving(); t += 100) advance(100);
}

/**
 * A frame clock. It calls tick(dtMs) while it runs and the scene is on screen.
 * The pause button toggles it. With reduced motion it starts paused, and step() moves it by hand.
 */
export function clock(root: HTMLElement, tick: (dt: number) => void, opts: { speed?: number } = {}) {
	const speed = opts.speed ?? 1;
	const pauseBtn = root.querySelector<HTMLButtonElement>('[data-act="pause"]');
	const stepBtn = root.querySelector<HTMLButtonElement>('[data-act="step"]');
	let paused = reducedMotion();
	let visible = true;
	let raf = 0;
	let last = 0;

	const label = () => {
		if (!pauseBtn) return;
		pauseBtn.textContent = paused ? 'Play' : 'Pause';
		pauseBtn.setAttribute('aria-pressed', String(paused));
		if (stepBtn) stepBtn.hidden = !paused;
	};

	const frame = (now: number) => {
		raf = 0;
		if (paused || !visible) return;
		const dt = Math.min(now - last, 100);
		last = now;
		tick(dt * speed);
		raf = requestAnimationFrame(frame);
	};

	const run = () => {
		if (raf || paused || !visible) return;
		last = performance.now();
		raf = requestAnimationFrame(frame);
	};

	pauseBtn?.addEventListener('click', () => {
		paused = !paused;
		label();
		run();
	});
	stepBtn?.addEventListener('click', () => tick(1000 * speed));

	new IntersectionObserver(
		(entries) => {
			visible = entries.some((e) => e.isIntersecting);
			run();
		},
		{ threshold: 0 },
	).observe(root);

	label();
	run();

	return {
		get paused() {
			return paused;
		},
		/** Run the clock again after something woke the scene, such as a button press. */
		wake: run,
	};
}

/** A polite live region that does not repeat the same message and waits between messages. */
export function announcer(root: HTMLElement) {
	const el = root.querySelector<HTMLElement>('[data-live]');
	let lastMsg = '';
	let lastAt = 0;
	return (msg: string) => {
		const now = performance.now();
		if (!el || msg === lastMsg || now - lastAt < 700) return;
		lastMsg = msg;
		lastAt = now;
		el.textContent = msg;
	};
}

/** The line under the stage that shows the Go call the last button made, with a short flash. */
export function caller(root: HTMLElement) {
	const el = root.querySelector<HTMLElement>('[data-call]');
	return (text: string) => {
		if (!el) return;
		setCall(el, text);
		el.classList.remove('is-new');
		void el.offsetWidth;
		el.classList.add('is-new');
	};
}

export function setText(el: Element | null | undefined, text: string) {
	if (el && el.textContent !== text) el.textContent = text;
}
