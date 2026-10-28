// Argument order: NewWorker, NewErrWorker and NewResultWorker read config ...any by type,
// from left to right (mergeConfigs in config.go). The last argument that sets a value wins.

import { escapeHtml as esc, highlightGo } from '../ui/go';
import { caller, qs } from './scene';

type Kind = 'int' | 'conc' | 'string' | 'name' | 'autorun' | 'other';

interface Arg {
	id: string;
	src: string;
	kind: Kind;
	value?: number | string | boolean;
	on: boolean;
}

const START: Arg[] = [
	{ id: 'a', src: '2', kind: 'int', value: 2, on: true },
	{ id: 'b', src: 'varmq.WithConcurrency(8)', kind: 'conc', value: 8, on: true },
	{ id: 'c', src: '"mailer"', kind: 'string', value: 'mailer', on: true },
	{ id: 'd', src: 'varmq.WithName("billing")', kind: 'name', value: 'billing', on: false },
	{ id: 'e', src: 'int64(4)', kind: 'other', on: true },
	{ id: 'f', src: '0', kind: 'int', value: 0, on: false },
	{ id: 'g', src: 'varmq.WithAutoRun(false)', kind: 'autorun', value: false, on: false },
];

document.querySelectorAll<HTMLElement>('[data-scene="gb-args"]').forEach(init);

function init(root: HTMLElement) {
	let args: Arg[] = START.map((a) => ({ ...a }));
	const say = caller(root);
	const listEl = qs(root, '[data-args]');
	const traceEl = qs(root, '[data-trace]');
	const resEl = qs(root, '[data-res]');
	const codeEl = qs(root, '[data-code]');
	let focusId = '';

	const render = () => {
		listEl.innerHTML = args
			.map(
				(a, i) => `<li class="gb-arg" data-on="${a.on}" data-id="${a.id}">
					<input type="checkbox" ${a.on ? 'checked' : ''} aria-label="Include ${esc(a.src)}" />
					<code>${esc(a.src)}</code>
					<span class="gb-arg__move">
						<button type="button" class="pbtn" data-move="-1" aria-label="Move ${esc(a.src)} earlier" ${i === 0 ? 'disabled' : ''}>Up</button>
						<button type="button" class="pbtn" data-move="1" aria-label="Move ${esc(a.src)} later" ${i === args.length - 1 ? 'disabled' : ''}>Down</button>
					</span>
				</li>`,
			)
			.join('');
		if (focusId) {
			listEl.querySelector<HTMLElement>(`[data-id="${focusId}"] [data-move]:not([disabled])`)?.focus();
		}

		const used = args.filter((a) => a.on);
		codeEl.innerHTML = highlightGo(
			used.length ? `worker := varmq.NewErrWorker(send,\n${used.map((a) => '\t' + a.src + ',').join('\n')}\n)` : 'worker := varmq.NewErrWorker(send)',
		);

		let conc: string | number = 1;
		let concFrom = 'the default';
		let name = '';
		let auto = true;
		const trace: string[] = [];
		for (const a of used) {
			let effect = '';
			let kind = 'set';
			if (a.kind === 'int' || a.kind === 'conc') {
				const v = a.value as number;
				const next = v < 1 ? 'one per CPU' : v;
				effect = `sets the concurrency to <b>${next}</b>${concFrom !== 'the default' ? `, replacing the one from ${concFrom}` : ''}`;
				conc = next;
				concFrom = a.src;
			} else if (a.kind === 'string' || a.kind === 'name') {
				effect = `names the worker <b>${esc(String(a.value))}</b>${name ? `, replacing ${esc(name)}` : ''}`;
				name = String(a.value);
			} else if (a.kind === 'autorun') {
				effect = 'sets auto-run to <b>false</b>';
				auto = false;
			} else {
				effect = '<b>ignored</b>, with no error: only a value of type <code>int</code> sets the concurrency';
				kind = 'ignored';
			}
			trace.push(`<li data-effect="${kind}"><code>${esc(a.src)}</code><span>${effect}</span></li>`);
		}
		traceEl.innerHTML = trace.length ? trace.join('') : '<li><span>No arguments: every default.</span></li>';
		resEl.innerHTML = `<div><dt>concurrency</dt><dd>${conc}</dd></div><div><dt>name</dt><dd>${name ? esc(name) : 'none'}</dd></div><div><dt>after the call</dt><dd>${auto ? 'Idle' : 'Initiated'}</dd></div>`;
	};

	root.addEventListener('click', (e) => {
		const t = e.target as HTMLElement;
		const li = t.closest<HTMLElement>('.gb-arg');
		const mv = t.closest<HTMLElement>('[data-move]');
		if (li && mv) {
			const i = args.findIndex((a) => a.id === li.dataset.id);
			const j = i + Number(mv.dataset.move);
			if (j >= 0 && j < args.length) {
				[args[i], args[j]] = [args[j], args[i]];
				focusId = args[j].id;
				say(`// ${args[j].src} is now ${j + 1}${['st', 'nd', 'rd'][j] ?? 'th'}`);
				render();
				focusId = '';
			}
		} else if (t.closest('[data-act="reset"]')) {
			args = START.map((a) => ({ ...a }));
			say('// the order and the tick marks are back to the start');
			render();
		}
	});

	root.addEventListener('change', (e) => {
		const t = e.target as HTMLInputElement;
		const li = t.closest<HTMLElement>('.gb-arg');
		if (!li || t.type !== 'checkbox') return;
		const a = args.find((x) => x.id === li.dataset.id)!;
		a.on = t.checked;
		say(a.on ? `// ${a.src} is in the call` : `// ${a.src} is out of the call`);
		render();
	});

	render();
}
