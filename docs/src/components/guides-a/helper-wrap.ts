// Scene: what Func, ErrFunc and ResultFunc wrap. The reader adds a closure, and the scene walks the lines
// of the worker function that the helper returns (main.go in the library), then shows what the reader gets.
import { caption, codeLines, q, reduced, say, tabs, Timers } from './kit';

type Helper = 'func' | 'err' | 'result';
type Closure = 'works' | 'fails' | 'nil';

const NIL_ERR = 'provided function is nil';

// The source of each helper, from main.go.
const SRC: Record<Helper, string[]> = {
	func: [
		'func(j varmq.Job[func()]) {',
		'\tif fn := j.Data(); fn != nil {',
		'\t\tfn()',
		'\t\treturn',
		'\t}',
		'\tpanic(errNilFunction)',
		'}',
	],
	err: [
		'func(j varmq.Job[func() error]) error {',
		'\tif fn := j.Data(); fn != nil {',
		'\t\treturn fn()',
		'\t}',
		'\treturn errNilFunction',
		'}',
	],
	result: [
		'func(j varmq.Job[func() (R, error)]) (R, error) {',
		'\tif fn := j.Data(); fn != nil {',
		'\t\treturn fn()',
		'\t}',
		'\treturn *new(R), errNilFunction',
		'}',
	],
};

// The lines that run when the closure is not nil, and when it is.
const PATH: Record<Helper, { ok: number[]; nil: number[] }> = {
	func: { ok: [0, 1, 2], nil: [0, 1, 5] },
	err: { ok: [0, 1, 2], nil: [0, 1, 4] },
	result: { ok: [0, 1, 2], nil: [0, 1, 4] },
};

const ADD: Record<Helper, Record<Closure, string[]>> = {
	func: {
		works: ['queue.Add(func() {', '\tfmt.Println("hello")', '})'],
		fails: ['queue.Add(func() {', '\tpanic("disk full")', '})'],
		nil: ['queue.Add(nil)'],
	},
	err: {
		works: ['queue.Add(func() error {', '\treturn nil', '})'],
		fails: ['queue.Add(func() error {', '\treturn errors.New("disk full")', '})'],
		nil: ['queue.Add(nil)'],
	},
	result: {
		works: ['queue.Add(func() (string, error) {', '\treturn "payload", nil', '})'],
		fails: ['queue.Add(func() (string, error) {', '\treturn "", errors.New("disk full")', '})'],
		nil: ['queue.Add(nil)'],
	},
};

const WORKER: Record<Helper, string> = {
	func: 'queue := varmq.NewWorker(varmq.Func(), 1).BindQueue()',
	err: 'queue := varmq.NewErrWorker(varmq.ErrFunc(), 1).BindQueue()',
	result: 'queue := varmq.NewResultWorker(varmq.ResultFunc[string](), 1).BindQueue()',
};

interface Out {
	type: string;
	readK: string;
	read: [string, string];
	errs: [string, string];
	why: string;
	note: string;
}

function outcome(h: Helper, c: Closure): Out {
	const panicText = (m: string) => `panic recovered inside worker: ${m}`;
	if (h === 'func') {
		const m = c === 'nil' ? NIL_ERR : 'disk full';
		return {
			type: 'EnqueuedJob, true',
			readK: 'job.Wait()',
			read: ['returns when the job ends. There is no outcome to read.', 'dim'],
			errs: c === 'works' ? ['nothing', 'dim'] : [panicText(m), 'bad'],
			why:
				c === 'works'
					? 'The closure ran and printed hello.'
					: c === 'nil'
						? 'Func panics on a nil closure. The worker recovers the panic, so the program carries on. A plain handle has no place for it, so it goes to Errs().'
						: 'The closure panicked. The worker recovers it and reports it on Errs(). The handle does not get it.',
			note: c === 'nil' ? 'The nil check fails, so the panic line runs.' : 'The check passes, so the helper calls fn().',
		};
	}
	const type = h === 'err' ? 'EnqueuedErrJob, true' : 'EnqueuedResultJob[string], true';
	const e = c === 'works' ? '' : c === 'nil' ? NIL_ERR : 'disk full';
	const read: [string, string] =
		h === 'err' ? (e ? [e, 'bad'] : ['<nil>', 'ok']) : e ? [`"", ${e}`, 'bad'] : ['"payload", <nil>', 'ok'];
	return {
		type,
		readK: h === 'err' ? 'job.Err()' : 'job.Result()',
		read,
		errs: e ? [e, 'bad'] : ['nothing', 'dim'],
		why: e
			? c === 'nil'
				? `${h === 'err' ? 'ErrFunc returns an error' : 'ResultFunc returns the zero value and an error'} for a nil closure, so nothing panics.`
				: `${h === 'err' ? 'Err()' : 'Result()'} returns what the closure returned${h === 'result' ? ', with the zero value of string' : ''}.`
			: `${h === 'err' ? 'Err()' : 'Result()'} returns what the closure returned.`,
		note: c === 'nil' ? 'The nil check fails, so the helper returns the error.' : 'The check passes, so the helper returns whatever fn() returns.',
	};
}

function init(root: HTMLElement) {
	const timers = new Timers();
	const el = (s: string) => q<HTMLElement>(root, s);
	const code = el('[data-code]');
	const addCode = el('[data-add-code]');
	const panels = Array.from(root.querySelectorAll<HTMLElement>('[data-step]'));
	let helper: Helper = 'func';
	let closure: Closure = 'works';

	const set = (node: HTMLElement, [text, tone]: [string, string]) => {
		node.textContent = text;
		node.dataset.tone = tone;
	};

	/** `lit` is how many lines of the path show as run, and `stage` is how many panels are lit. */
	const paint = (lit: number, stage: number) => {
		const o = outcome(helper, closure);
		const path = closure === 'nil' ? PATH[helper].nil : PATH[helper].ok;
		code.innerHTML = codeLines(SRC[helper].join('\n'));
		code.querySelectorAll<HTMLElement>('.ga-line').forEach((l, i) => {
			const idx = path.indexOf(i);
			l.classList.toggle('is-on', idx >= 0 && idx < lit);
			l.classList.toggle('is-skip', lit >= path.length && idx < 0);
		});
		addCode.innerHTML = codeLines(ADD[helper][closure].join('\n'));
		el('[data-type]').textContent = o.type;
		el('[data-read-k]').textContent = o.readK;
		set(el('[data-read]'), o.read);
		set(el('[data-errs]'), o.errs);
		el('[data-why]').textContent = o.why;
		el('[data-line-note]').textContent = lit >= path.length ? o.note : '';
		panels.forEach((p) => p.toggleAttribute('data-on', stage >= Number(p.dataset.step)));
	};

	const announce = () => {
		const o = outcome(helper, closure);
		say(root, `${helper === 'func' ? 'Func' : helper === 'err' ? 'ErrFunc' : 'ResultFunc'} with ${closure === 'nil' ? 'a nil closure' : `a closure that ${closure === 'works' ? 'works' : 'fails'}`}. Add returned ${o.type}. ${o.readK}: ${o.read[0]}. Errs: ${o.errs[0]}. ${o.why}`);
	};

	const run = (animated: boolean) => {
		timers.clear();
		caption(root, WORKER[helper]);
		const path = closure === 'nil' ? PATH[helper].nil : PATH[helper].ok;
		if (!animated || reduced()) {
			paint(path.length, 3);
			announce();
			return;
		}
		paint(0, 1);
		path.forEach((_, i) => timers.after(450 + i * 420, () => paint(i + 1, 2)));
		timers.after(450 + path.length * 420 + 120, () => {
			paint(path.length, 3);
			announce();
		});
	};

	const panel = el('[data-panel]');
	tabs(q(root, '[role="tablist"]'), (k) => {
		helper = k as Helper;
		panel.setAttribute('aria-labelledby', `ga-fh-tab-${k}`);
		run(false);
	});
	tabs(q(root, '[role="radiogroup"]'), (k) => {
		closure = k as Closure;
		run(false);
	});
	q(root, '[data-add]').addEventListener('click', () => run(true));

	run(false);
	say(root, '');
	root.dataset.ready = '';
}

document.querySelectorAll<HTMLElement>('[data-ga="helper-wrap"]').forEach(init);
