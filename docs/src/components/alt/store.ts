// Storage demo: what a process restart does to waiting jobs, per storage choice.

type Mode = 'memory' | 'redis' | 'sqlite';

const NAMES: Record<Mode, string> = { memory: 'In memory', redis: 'Redis', sqlite: 'SQLite' };
const SHOW = 14;

const root = document.querySelector<HTMLElement>('[data-store]');
if (root) init(root);

function init(root: HTMLElement) {
	const $ = <T extends HTMLElement>(s: string) => root.querySelector<T>(s)!;
	const reduce = window.matchMedia('(prefers-reduced-motion: reduce)');

	const proc = $('[data-proc]');
	const procState = $('[data-proc-state]');
	const runEl = $('[data-run]');
	const memQ = $('[data-mem-q]');
	const db = $('[data-db]');
	const dbName = $('[data-db-name]');
	const dbQ = $('[data-db-q]');
	const dbEmpty = $('[data-db-empty]');
	const result = $('[data-result]');
	const pauseBtn = $<HTMLButtonElement>('[data-act="freeze"]');

	let mode: Mode = 'memory';
	let waiting: number[] = [];
	let running: number | null = null;
	let lost: number[] = [];
	let nextId = 1;
	let up = true;
	let frozen = reduce.matches;
	let visible = false;
	let timer = 0;

	const fill = (n: number) => {
		for (let i = 0; i < n; i++) waiting.push(nextId++);
	};

	const chip = (id: number, cls = '') => {
		const s = document.createElement('span');
		s.className = `job ${cls}`.trim();
		s.textContent = String(id);
		return s;
	};

	const list = (ids: number[], cls = '') => {
		const nodes = ids.slice(0, SHOW).map((id) => chip(id, cls));
		if (ids.length > SHOW) {
			const more = document.createElement('span');
			more.className = 'job job--more';
			more.textContent = `+${ids.length - SHOW}`;
			nodes.push(more);
		}
		return nodes;
	};

	const render = () => {
		const persistent = mode !== 'memory';
		procState.textContent = up ? (running === null ? 'idle' : 'running') : 'restarting';
		proc.toggleAttribute('data-down', !up);
		dbName.textContent = persistent ? NAMES[mode] : 'Storage';
		db.toggleAttribute('data-off', !persistent);
		dbEmpty.hidden = persistent;
		memQ.replaceChildren(...(persistent ? [] : [...list(waiting), ...list(lost, 'is-lost')]));
		dbQ.replaceChildren(...(persistent ? list(waiting) : []));
		runEl.replaceChildren(running === null ? Object.assign(document.createElement('small'), { textContent: up ? 'no job' : 'down' }) : chip(running));
		pauseBtn.textContent = frozen ? 'Resume demo' : 'Pause demo';
		pauseBtn.setAttribute('aria-pressed', String(frozen));
	};

	const tick = () => {
		timer = 0;
		if (frozen || !visible) return;
		if (up) {
			running = null;
			if (waiting.length) running = waiting.shift()!;
			else {
				fill(6);
				lost = [];
			}
		}
		render();
		timer = window.setTimeout(tick, reduce.matches ? 1800 : 1000);
	};
	const start = () => {
		if (!timer && !frozen && visible) timer = window.setTimeout(tick, 600);
	};

	const restart = () => {
		if (!up) return;
		const persistent = mode !== 'memory';
		const waitingCount = waiting.length;
		const hadRunning = running !== null;
		up = false;
		if (persistent) {
			// The job that was running is not acknowledged, so it can run again.
			if (running !== null) waiting.unshift(running);
			running = null;
			result.textContent = `${waitingCount} waiting ${waitingCount === 1 ? 'job stays' : 'jobs stay'} in ${NAMES[mode]}${hadRunning ? '. The running job was not acknowledged, so it can run again' : ''}.`;
		} else {
			lost = [...waiting, ...(running !== null ? [running] : [])];
			waiting = [];
			running = null;
			result.textContent = `${lost.length} ${lost.length === 1 ? 'job is' : 'jobs are'} lost. A queue in memory ends with the process.`;
		}
		render();
		window.setTimeout(
			() => {
				up = true;
				if (mode === 'memory') lost = [];
				render();
				start();
			},
			reduce.matches ? 1200 : 1400,
		);
	};

	root.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLElement>('[data-act]');
		if (!btn) return;
		if (btn.dataset.act === 'restart') restart();
		if (btn.dataset.act === 'add') {
			fill(5);
			lost = [];
			result.textContent = `5 jobs added. ${waiting.length} waiting.`;
			render();
		}
		if (btn.dataset.act === 'freeze') {
			frozen = !frozen;
			render();
			if (frozen) {
				window.clearTimeout(timer);
				timer = 0;
			} else start();
		}
	});

	document.addEventListener('tabchange', (e) => {
		const tab = e.target as HTMLElement;
		if (!tab.closest('[data-store-tabs]')) return;
		mode = (tab.dataset.key as Mode) ?? 'memory';
		waiting = [];
		lost = [];
		running = null;
		up = true;
		fill(6);
		result.textContent = '';
		render();
	});

	new IntersectionObserver(([entry]) => {
		visible = entry.isIntersecting;
		if (visible) start();
	}).observe(root);

	fill(6);
	render();
}
