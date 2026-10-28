// API explorer: renders the workers, sends the typed request to the model, shows the reply.

import { add, handle, seed, tick, type Reply, type Worker } from './api-model';
import { h } from '../ui/dom';
import { escapeHtml, highlightJson, highlightShell } from '../ui/go';

const root = document.querySelector<HTMLElement>('[data-api]');
if (root) init(root);

function init(root: HTMLElement) {
	const figure = root.closest<HTMLElement>('.sframe')!;
	const $ = <T extends HTMLElement>(s: string) => figure.querySelector<T>(s)!;
	const workersEl = $('[data-workers]');
	const statusEl = $('[data-status]');
	const wire = $('[data-wire]');
	const why = $('[data-why]');
	const call = $('[data-call]');
	const method = $<HTMLSelectElement>('[data-method-input]');
	const path = $<HTMLInputElement>('[data-path-input]');
	const pretty = $<HTMLInputElement>('[data-pretty]');
	const copy = $<HTMLButtonElement>('[data-act="copy"]');

	let workers = seed();
	let last: Reply | null = null;
	let lastReq = '';
	let changed = new Set<string>();

	const curl = (m: string, p: string) => (m === 'GET' ? `curl -i localhost:8080${p}` : `curl -i -X ${m} localhost:8080${p}`);

	function slots(w: Worker) {
		const n = Math.max(w.concurrency, w.processing);
		return h(
			'span',
			{ class: 'api__slots', 'aria-hidden': 'true' },
			...Array.from({ length: Math.min(n, 12) }, (_, i) => h('i', { class: i < w.processing ? 'on' : '' })),
		);
	}

	function renderWorkers() {
		workersEl.replaceChildren(
			...Array.from(workers.values()).map((w) =>
				h(
					'div',
					{ class: `api__worker${changed.has(w.name) ? ' is-changed' : ''}`, role: 'listitem' },
					h('code', { class: 'api__wname', text: w.name }),
					h('span', { class: `api__state api__state--${w.status.toLowerCase()}`, text: w.status }),
					slots(w),
					h('span', { class: 'api__nums', text: `limit ${w.concurrency} · running ${w.processing} · waiting ${w.pending} · done ${w.completed}` }),
				),
			),
		);
	}

	function renderReply() {
		if (!last) return;
		const r = last;
		const cls = r.code < 300 ? 'ok' : r.code < 500 ? 'warn' : 'bad';
		statusEl.replaceChildren(
			h('span', { class: 'api__req-line' }, h('b', { text: lastReq.split(' ')[0] }), ' ', h('code', { text: lastReq.split(' ').slice(1).join(' ') })),
			h('span', { class: `api__code api__code--${cls}`, text: `${r.code} ${r.reason}` }),
		);
		let body = r.body;
		if (r.json && pretty.checked) {
			try {
				body = JSON.stringify(JSON.parse(r.body), null, 2);
			} catch {
				/* keep the compact text */
			}
		}
		const head = `HTTP/1.1 ${r.code} ${r.reason}\n` + r.headers.map(([k, v]) => `<span class="tk-t">${escapeHtml(k)}</span>: ${escapeHtml(v)}`).join('\n');
		const bodyHtml = r.json ? highlightJson(body) : escapeHtml(body);
		wire.innerHTML = head + (body ? `\n\n${bodyHtml}` : '\n\n<span class="tk-c">(no body)</span>');
		why.replaceChildren(h('b', { text: 'Why. ' }), r.why, r.waited ? ' The request waited for the running jobs.' : '');
	}

	function send(m: string, p: string) {
		const before = new Map(Array.from(workers, ([k, v]) => [k, JSON.stringify(v)]));
		last = handle(workers, m, p.trim());
		lastReq = `${m} ${p.trim()}`;
		changed = new Set(Array.from(workers).filter(([k, v]) => before.get(k) !== JSON.stringify(v)).map(([k]) => k));
		method.value = m;
		path.value = p.trim();
		call.innerHTML = highlightShell(curl(m, p.trim()));
		renderWorkers();
		renderReply();
	}

	root.addEventListener('click', (e) => {
		const chip = (e.target as HTMLElement).closest<HTMLElement>('[data-path]');
		if (chip) send(chip.dataset.method!, chip.dataset.path!);
	});
	$('[data-form]').addEventListener('submit', (e) => {
		e.preventDefault();
		send(method.value, path.value);
	});
	pretty.addEventListener('change', renderReply);

	figure.addEventListener('click', async (e) => {
		const el = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-act]');
		if (!el) return;
		const act = el.dataset.act;
		if (act === 'tick') {
			for (const w of workers.values()) tick(w);
			changed = new Set(workers.keys());
			renderWorkers();
		} else if (act === 'add') {
			add(workers.get('email-sender')!, 10);
			changed = new Set(['email-sender']);
			renderWorkers();
		} else if (act === 'reset') {
			workers = seed();
			changed = new Set();
			renderWorkers();
			send('GET', '/varmq/workers');
		} else if (act === 'copy') {
			const text = curl(method.value, path.value.trim());
			try {
				await navigator.clipboard.writeText(text);
				el.textContent = 'Copied';
			} catch {
				el.textContent = 'Copy failed';
			}
			window.setTimeout(() => (el.textContent = el.dataset.label ?? 'Copy as curl'), 1600);
		}
	});

	renderWorkers();
	send('GET', '/varmq/workers');
	changed = new Set();
	renderWorkers();
}
