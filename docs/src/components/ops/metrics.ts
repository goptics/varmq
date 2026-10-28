// Scrape replay: a clock drives the numbers of metrics-data.ts through the text, the chart and PromQL.

import { END, METRICS, sample, type Metric, type Sample } from './metrics-data';
import { h, reducedMotion } from './ops';

const SVG = 'http://www.w3.org/2000/svg';
const svg = (tag: string, attrs: Record<string, string | number> = {}, ...kids: Node[]) => {
	const el = document.createElementNS(SVG, tag);
	for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v));
	el.append(...kids);
	return el;
};

/** The value of a query that only reads gauges at this moment. */
const NOW: Record<string, (s: Sample) => string> = {
	foo_worker_waiting_total: (s) => String(s.waiting),
	foo_worker_workers_processing: (s) => String(s.processing),
	foo_worker_workers_idle: (s) => String(s.idle),
	'foo_worker_workers_processing / foo_worker_workers_concurrency': (s) => String(s.processing / s.concurrency),
	'avg_over_time(foo_worker_waiting_total[5m]) > 500': (s) => (s.waiting > 500 ? 'true at this scrape alone' : 'not above 500 at this scrape'),
};

const root = document.querySelector<HTMLElement>('[data-prom]');
if (root) init(root);

function init(root: HTMLElement) {
	const figure = root.closest<HTMLElement>('.ops-scene')!;
	const slider = root.querySelector<HTMLInputElement>('[data-t]')!;
	const tOut = root.querySelector<HTMLElement>('[data-t-out]')!;
	const detail = root.querySelector<HTMLElement>('[data-detail]')!;
	const play = root.querySelector<HTMLButtonElement>('[data-act="play"]')!;
	const restart = figure.querySelector<HTMLInputElement>('[data-restart]')!;
	const note = figure.querySelector<HTMLElement>('[data-note]')!;

	let t = 2;
	let key: keyof Sample = 'completed';
	let timer = 0;

	const restartAt = () => (restart.checked ? 5 : null);
	const stop = () => {
		if (timer) window.clearInterval(timer);
		timer = 0;
		play.textContent = t >= END ? 'Replay' : 'Play';
		play.setAttribute('aria-label', t >= END ? 'Replay from the start' : 'Play the replay');
	};

	function chart(m: Metric) {
		const W = 280;
		const H = 96;
		const left = 30;
		const top = 8;
		const bottom = 18;
		const pts: [number, number][] = [];
		for (let x = 0; x <= END * 10; x++) pts.push([x / 10, sample(x / 10, restartAt())[m.key]]);
		const ymax = Math.max(1, ...pts.map((p) => p[1]));
		const X = (sec: number) => left + (sec / END) * (W - left - 6);
		const Y = (v: number) => top + (1 - v / ymax) * (H - top - bottom);
		const d = pts.map(([sec, v], i) => `${i ? 'L' : 'M'}${X(sec).toFixed(1)} ${Y(v).toFixed(1)}`).join(' ');
		const now = sample(t, restartAt())[m.key];
		const label = `Chart of ${m.name} over ${END} seconds. It starts at ${pts[0][1]} and ends at ${pts[pts.length - 1][1]}. At ${t.toFixed(1)} seconds it reads ${now}.`;
		return svg(
			'svg',
			{ viewBox: `0 0 ${W} ${H}`, class: 'prom__chart', role: 'img', 'aria-label': label },
			svg('line', { x1: left, y1: Y(0), x2: W - 6, y2: Y(0), class: 'prom__axis' }),
			svg('line', { x1: left, y1: top, x2: left, y2: Y(0), class: 'prom__axis' }),
			...(restartAt() !== null ? [svg('line', { x1: X(5), y1: top, x2: X(5), y2: Y(0), class: 'prom__mark' })] : []),
			svg('path', { d, class: 'prom__path', fill: 'none' }),
			svg('line', { x1: X(t), y1: top, x2: X(t), y2: Y(0), class: 'prom__cursor' }),
			svg('circle', { cx: X(t), cy: Y(now), r: 3.5, class: 'prom__dot' }),
			Object.assign(svg('text', { x: left - 4, y: top + 8, 'text-anchor': 'end', class: 'prom__tick' }), { textContent: String(ymax) }),
			Object.assign(svg('text', { x: left - 4, y: Y(0), 'text-anchor': 'end', class: 'prom__tick' }), { textContent: '0' }),
			Object.assign(svg('text', { x: left, y: H - 4, class: 'prom__tick' }), { textContent: '0 s' }),
			Object.assign(svg('text', { x: W - 6, y: H - 4, 'text-anchor': 'end', class: 'prom__tick' }), { textContent: `${END} s` }),
			...(restartAt() !== null ? [Object.assign(svg('text', { x: X(5) + 3, y: top + 8, class: 'prom__tick' }), { textContent: 'restart' })] : []),
		);
	}

	function renderDetail(s: Sample) {
		const m = METRICS.find((x) => x.key === key)!;
		const now = s[m.key];
		detail.replaceChildren(
			h(
				'div',
				{ class: 'prom__head' },
				h('code', { class: 'prom__title', text: `foo_worker_${m.name}` }),
				h('span', { class: `prom__type prom__type--${m.type}`, text: m.type }),
			),
			h('p', { class: 'prom__p' }, 'From ', h('a', { href: m.href, text: m.from }), `. Now ${now}.`),
			h('p', { class: 'prom__p', text: m.meaning }),
			m.watch ? h('p', { class: 'prom__watch', text: m.watch }) : null,
			chart(m),
			h('p', { class: 'amap__h', text: 'PromQL' }),
			h(
				'ul',
				{ class: 'prom__queries' },
				...m.queries.map((q) =>
					h(
						'li',
						{},
						h('code', { class: 'prom__q', text: q.q }),
						h('span', { class: 'prom__says', text: q.says }),
						NOW[q.q] ? h('span', { class: 'prom__now', text: `At ${t.toFixed(1)} s: ${NOW[q.q](s)}` }) : null,
					),
				),
			),
		);
	}

	// On a narrow panel the detail opens under the metric you pressed. On a wide one it sits beside the list.
	function placeDetail() {
		const grid = root!.querySelector<HTMLElement>('.prom__grid')!;
		const pressed = root!.querySelector<HTMLElement>(`[data-metric="${key}"]`)!;
		if (root!.clientWidth < 640) {
			if (detail.previousElementSibling !== pressed) pressed.after(detail);
		} else if (detail.parentElement !== grid) {
			grid.append(detail);
		}
	}

	function render() {
		const s = sample(t, restartAt());
		for (const m of METRICS) {
			const el = root!.querySelector<HTMLElement>(`[data-val="${m.key}"]`);
			if (el) el.textContent = String(s[m.key]);
		}
		slider.value = String(t);
		tOut.textContent = `${t.toFixed(1)} s`;
		slider.setAttribute('aria-valuetext', `${t.toFixed(1)} seconds`);
		for (const b of root!.querySelectorAll<HTMLElement>('[data-metric]')) b.setAttribute('aria-pressed', String(b.dataset.metric === key));
		placeDetail();
		renderDetail(s);
		note.textContent = restartAt() !== null && t >= 5 ? 'The process restarted at 5 s. The counters began again at zero, and the program ran its 1000 jobs again.' : 'Numbers follow a real run of this program.';
	}

	root.addEventListener('click', (e) => {
		const b = (e.target as HTMLElement).closest<HTMLElement>('[data-metric]');
		if (b) {
			key = b.dataset.metric as keyof Sample;
			render();
		}
	});
	slider.addEventListener('input', () => {
		stop();
		t = Number(slider.value);
		render();
	});
	restart.addEventListener('change', render);
	play.addEventListener('click', () => {
		if (timer) return stop();
		if (t >= END) t = 0;
		play.textContent = 'Pause';
		play.setAttribute('aria-label', 'Pause the replay');
		const step = reducedMotion() ? 0.5 : 0.1;
		timer = window.setInterval(() => {
			t = Math.min(END, Math.round((t + step) * 10) / 10);
			render();
			if (t >= END) stop();
		}, step * 1000);
	});

	let narrow = root.clientWidth < 640;
	new ResizeObserver(() => {
		const now = root.clientWidth < 640;
		if (now !== narrow) {
			narrow = now;
			render();
		}
	}).observe(root);

	render();
}
