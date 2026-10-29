import { SCENES, type Frame } from './scene-data';

const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');
const toggle = document.querySelector<HTMLButtonElement>('[data-scenes-toggle]');
const players: { play: () => void; stop: () => void }[] = [];
let playing = !reduce.matches;

function apply(root: HTMLElement, frame: Frame) {
	for (const chip of root.querySelectorAll<HTMLElement>('[data-k]')) {
		const cell = frame.chips[chip.dataset.k!];
		if (!cell) continue;
		const [c, r, o, state = ''] = cell;
		chip.style.setProperty('--c', String(c));
		chip.style.setProperty('--r', String(r));
		chip.style.setProperty('--o', String(o));
		const [kind, n] = state.split(':');
		chip.classList.toggle('is-next', kind === 'next');
		chip.classList.toggle('is-run', kind === 'run');
		chip.classList.toggle('is-dim', kind === 'dim');
		if (n) chip.dataset.n = n;
		else delete chip.dataset.n;
	}
	const cap = root.closest('.scene-card')?.querySelector<HTMLElement>('[data-cap]');
	if (cap) cap.textContent = frame.cap;
	const w = root.querySelector<HTMLElement>('[data-w]');
	if (w && frame.w !== undefined) w.textContent = frame.w || 'worker';
}

function init(root: HTMLElement) {
	const frames = SCENES[root.dataset.scene ?? ''].frames;
	let i = 0;
	let timer = 0;
	let visible = false;
	let started = false;

	const tick = () => {
		timer = 0;
		if (!playing || !visible) return;
		i = (i + 1) % frames.length;
		if (i === 0) {
			// Snap back to the empty frame without a transition, then continue.
			root.classList.add('is-reset');
			apply(root, frames[0]);
			void root.offsetWidth;
			root.classList.remove('is-reset');
		} else {
			apply(root, frames[i]);
		}
		timer = window.setTimeout(tick, i === frames.length - 1 ? 2600 : 950);
	};

	const play = () => {
		if (timer || !playing || !visible) return;
		if (!started) {
			started = true;
			i = 0;
			root.classList.add('is-reset');
			apply(root, frames[0]);
			void root.offsetWidth;
			root.classList.remove('is-reset');
		}
		timer = window.setTimeout(tick, 450);
	};
	const stop = () => {
		window.clearTimeout(timer);
		timer = 0;
	};

	new IntersectionObserver(([entry]) => {
		visible = entry.isIntersecting;
		if (visible) play();
		else stop();
	}).observe(root);

	players.push({ play, stop });
}

document.querySelectorAll<HTMLElement>('[data-scene]').forEach(init);

const label = () => {
	if (!toggle) return;
	toggle.textContent = playing ? 'Pause animation' : 'Play animation';
};
label();
toggle?.addEventListener('click', () => {
	playing = !playing;
	label();
	for (const p of players) playing ? p.play() : p.stop();
});
