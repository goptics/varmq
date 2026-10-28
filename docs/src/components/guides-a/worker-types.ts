// Scene: the three worker constructors on the same function. The reader picks a constructor and a job,
// and sees what the function returns, what Add hands back, and what the reader can read afterwards.
import { caption, codeLines, h, q, reduced, say, tabs, Timers } from './kit';

type Kind = 'plain' | 'err' | 'result';
type Input = '42' | 'forty-two' | 'boom';

const ATOI_ERR = 'strconv.Atoi: parsing "forty-two": invalid syntax';

const SRC: Record<Kind, string[]> = {
	plain: [
		'worker := varmq.NewWorker(func(j varmq.Job[string]) {',
		'\tif j.Data() == "boom" {',
		'\t\tpanic("boom")',
		'\t}',
		'\tn, _ := strconv.Atoi(j.Data())',
		'\tfmt.Println("parsed", n)',
		'})',
	],
	err: [
		'worker := varmq.NewErrWorker(func(j varmq.Job[string]) error {',
		'\tif j.Data() == "boom" {',
		'\t\tpanic("boom")',
		'\t}',
		'\t_, err := strconv.Atoi(j.Data())',
		'\treturn err',
		'})',
	],
	result: [
		'worker := varmq.NewResultWorker(func(j varmq.Job[string]) (int, error) {',
		'\tif j.Data() == "boom" {',
		'\t\tpanic("boom")',
		'\t}',
		'\treturn strconv.Atoi(j.Data())',
		'})',
	],
};

// The lines that run, for each input. The first line is the signature and always runs.
const RUNS: Record<Kind, Record<Input, number[]>> = {
	plain: { '42': [0, 1, 4, 5, 6], 'forty-two': [0, 1, 4, 5, 6], boom: [0, 1, 2] },
	err: { '42': [0, 1, 4, 5, 6], 'forty-two': [0, 1, 4, 5, 6], boom: [0, 1, 2] },
	result: { '42': [0, 1, 4, 5], 'forty-two': [0, 1, 4, 5], boom: [0, 1, 2] },
};

const PANIC = (k: Kind) => `panic recovered inside ${k === 'plain' ? 'worker' : k === 'err' ? 'err-worker' : 'result-worker'}: boom`;

interface View {
	ret: [string, string];
	type: string;
	methods: { text: string; hot?: boolean }[];
	readK: string;
	read: [string, string];
	errs: [string, string];
	why: string;
}

const COMMON = ['ID()', 'Status()', 'IsClosed()', 'Wait()', 'Close()'];

function view(k: Kind, i: Input): View {
	const boom = i === 'boom';
	const bad = i === 'forty-two';
	const base = COMMON.map((text) => ({ text }));
	if (k === 'plain') {
		return {
			ret: boom ? ['it panicked: boom', 'bad'] : [`nothing. It printed "parsed ${bad ? 0 : 42}"${bad ? ': the error was dropped' : ''}`, ''],
			type: 'EnqueuedJob',
			methods: base,
			readK: 'job outcome',
			read: ['nothing to read', 'dim'],
			errs: boom ? [PANIC(k), 'bad'] : ['nothing', 'dim'],
			why: boom
				? 'The worker recovers the panic and reports it on Errs(). The handle does not get it.'
				: 'A plain function cannot return an error. The handle only lets you Wait, read the status or close the job.',
		};
	}
	if (k === 'err') {
		const e = boom ? PANIC(k) : bad ? ATOI_ERR : '';
		return {
			ret: boom ? ['it panicked: boom', 'bad'] : bad ? [`error: ${ATOI_ERR}`, 'bad'] : ['nil', 'ok'],
			type: 'EnqueuedErrJob',
			methods: [...base, { text: 'Err() error', hot: true }],
			readK: 'job.Err()',
			read: e ? [e, 'bad'] : ['<nil>', 'ok'],
			errs: e ? [e, 'bad'] : ['nothing', 'dim'],
			why: e ? 'Err() and Errs() both carry the failure, so one failure shows up in two places.' : 'nil means the function reported no error.',
		};
	}
	const e = boom ? PANIC(k) : bad ? ATOI_ERR : '';
	return {
		ret: boom ? ['it panicked: boom', 'bad'] : bad ? [`0, ${ATOI_ERR}`, 'bad'] : ['42, nil', 'ok'],
		type: 'EnqueuedResultJob[int]',
		methods: [...base, { text: 'Result() (int, error)', hot: true }],
		readK: 'job.Result()',
		read: e ? [`0, ${e}`, 'bad'] : ['42, <nil>', 'ok'],
		errs: e ? [e, 'bad'] : ['nothing', 'dim'],
		why: e
			? 'On an error or a panic, Result() returns the zero value of int and the error.'
			: 'Result() returns both values of the function.',
	};
}

function init(root: HTMLElement) {
	const timers = new Timers();
	const code = q<HTMLElement>(root, '[data-code]');
	const panels = Array.from(root.querySelectorAll<HTMLElement>('[data-step]'));
	const el = (s: string) => q<HTMLElement>(root, s);
	let kind: Kind = 'plain';
	let input: Input = '42';

	const set = (node: HTMLElement, [text, tone]: [string, string]) => {
		node.textContent = text;
		node.dataset.tone = tone;
	};

	const paint = (lit: number) => {
		const v = view(kind, input);
		code.innerHTML = codeLines(SRC[kind].join('\n'));
		const run = new Set(RUNS[kind][input]);
		code.querySelectorAll<HTMLElement>('.ga-line').forEach((l, n) => {
			l.classList.toggle('is-on', lit >= 1 && run.has(n) && n > 0);
			l.classList.toggle('is-skip', lit >= 1 && !run.has(n) && n > 0);
		});
		set(el('[data-ret]'), v.ret);
		el('[data-ret-k]').textContent = 'returns';
		el('[data-type]').textContent = v.type;
		el('[data-ok]').textContent = 'true';
		const methods = el('[data-methods]');
		methods.replaceChildren(...v.methods.map((m) => h('li', { class: `ga-tag${m.hot ? ' ga-tag--on' : ''}`, text: m.text })));
		el('[data-read-k]').textContent = v.readK;
		set(el('[data-read]'), v.read);
		set(el('[data-errs]'), v.errs);
		el('[data-why]').textContent = v.why;
		panels.forEach((p, i) => p.toggleAttribute('data-on', lit > i));
	};

	const names: Record<Kind, string> = { plain: 'NewWorker', err: 'NewErrWorker', result: 'NewResultWorker' };
	const announce = () => {
		const v = view(kind, input);
		say(root, `${names[kind]} with the data "${input}". The function ${v.ret[0]}. Add returned ${v.type} and true. You read: ${v.read[0]}. Errs: ${v.errs[0]}.`);
	};

	const run = (animated: boolean) => {
		timers.clear();
		caption(root, `job, ok := queue.Add("${input}")`);
		if (!animated || reduced()) {
			paint(3);
			announce();
			return;
		}
		paint(0);
		panels.forEach((p) => p.removeAttribute('data-on'));
		for (const n of [1, 2, 3]) timers.after(n * 420, () => paint(n));
		timers.after(3 * 420 + 50, announce);
	};

	const panel = el('[data-panel]');
	tabs(q(root, '[role="tablist"]'), (k) => {
		kind = k as Kind;
		panel.setAttribute('aria-labelledby', `ga-wt-tab-${k}`);
		run(false);
	});
	tabs(q(root, '[role="radiogroup"]'), (k) => {
		input = k as Input;
		run(false);
	});
	q(root, '[data-add]').addEventListener('click', () => run(true));

	run(false);
	say(root, '');
	root.dataset.ready = '';
}

document.querySelectorAll<HTMLElement>('[data-ga="worker-types"]').forEach(init);
