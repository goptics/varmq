// Shared behavior for the guide scenes: a clock with a pause button, radio groups,
// a polite live region, and small DOM helpers. Each scene owns one root element.

export const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export const qs = <T extends HTMLElement>(root: ParentNode, sel: string) => root.querySelector<T>(sel)!;
export const qsa = <T extends HTMLElement>(root: ParentNode, sel: string) => Array.from(root.querySelectorAll<T>(sel));

export function esc(s: string) {
	return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

/** Seconds with one decimal: 2400 -> "2.4 s". */
export const sec = (ms: number) => `${(ms / 1000).toFixed(1)} s`;

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

/** A radio group of buttons with roving tabindex and arrow keys. */
export function radios(group: HTMLElement, onChange: (value: string) => void) {
	const items = qsa<HTMLButtonElement>(group, '[role="radio"]');
	const select = (item: HTMLElement, focus = false, notify = true) => {
		for (const i of items) {
			const on = i === item;
			i.setAttribute('aria-checked', String(on));
			i.tabIndex = on ? 0 : -1;
		}
		if (focus) item.focus();
		if (notify) onChange(item.dataset.value ?? '');
	};
	group.addEventListener('click', (e) => {
		const item = (e.target as HTMLElement).closest<HTMLElement>('[role="radio"]');
		if (item && group.contains(item) && !(item as HTMLButtonElement).disabled) select(item);
	});
	group.addEventListener('keydown', (e) => {
		const i = items.indexOf(document.activeElement as HTMLButtonElement);
		if (i < 0) return;
		let next = -1;
		if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (i + 1) % items.length;
		else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (i - 1 + items.length) % items.length;
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = items.length - 1;
		if (next >= 0) {
			e.preventDefault();
			select(items[next], true);
		}
	});
	return {
		set(value: string, notify = false) {
			const item = items.find((i) => i.dataset.value === value);
			if (item) select(item, false, notify);
		},
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

/** The line under the scene that shows the Go call a button made, with a short flash. */
export function caller(root: HTMLElement) {
	const el = root.querySelector<HTMLElement>('[data-call]');
	return (text: string) => {
		if (!el) return;
		el.textContent = text;
		el.classList.remove('is-new');
		void el.offsetWidth;
		el.classList.add('is-new');
	};
}

export function setText(el: Element | null | undefined, text: string) {
	if (el && el.textContent !== text) el.textContent = text;
}
