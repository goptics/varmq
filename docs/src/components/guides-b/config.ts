// Option explorer: the reader ticks options and the page builds the NewWorker call
// and says what the worker gets. Defaults and rules are the ones in config.go.

import { tabs } from '../ui/dom';
import { highlightGo } from '../ui/go';
import { caller, qs, qsa } from './scene';

type Ctor = 'NewWorker' | 'NewErrWorker' | 'NewResultWorker';
const FUNC: Record<Ctor, string> = { NewWorker: 'process', NewErrWorker: 'send', NewResultWorker: 'build' };

document.querySelectorAll<HTMLElement>('[data-scene="gb-config"]').forEach(init);

function init(root: HTMLElement) {
	let ctor: Ctor = 'NewWorker';
	const say = caller(root);
	const codeEl = qs(root, '[data-code]');
	const resEl = qs(root, '[data-res]');
	const opts = qsa<HTMLInputElement>(root, '[data-opt]');
	const on = (k: string) => qs<HTMLInputElement>(root, `[data-opt="${k}"]`).checked;
	const val = (k: string) => qs<HTMLInputElement | HTMLSelectElement>(root, `[data-val="${k}"]`).value;
	const as = (k: string) => qs<HTMLSelectElement>(root, `[data-as="${k}"]`).value;

	const build = () => {
		const args: string[] = [];
		const conc = Number(val('concurrency'));
		if (on('concurrency')) args.push(as('concurrency') === 'int' ? `${conc}, // concurrency` : `varmq.WithConcurrency(${conc}),`);
		if (on('name')) args.push(as('name') === 'string' ? `"${val('name')}", // name` : `varmq.WithName("${val('name')}"),`);
		if (on('autoRun')) args.push('varmq.WithAutoRun(false),');
		if (on('strategy')) args.push(`varmq.WithStrategy(varmq.${val('strategy')}),`);
		if (on('ratio')) args.push(`varmq.WithMinIdleWorkerRatio(${val('ratio')}),`);
		if (on('expiry')) args.push(`varmq.WithIdleWorkerExpiryDuration(${val('expiry')}*time.Second),`);
		if (on('errHandler')) args.push('varmq.WithErrHandler(logErr),');
		if (on('context')) args.push('varmq.WithContext(ctx),');
		if (on('retention')) args.push(`varmq.WithStoppedRetention(${val('retention')}*time.Minute),`);
		if (on('idGen')) args.push('varmq.WithJobIdGenerator(newID),');

		const head = on('defaults') ? 'varmq.DefaultConcurrency(8) // once, before any worker is created\n\n' : '';
		const call = args.length
			? `worker := varmq.${ctor}(${FUNC[ctor]},\n${args.map((a) => '\t' + a).join('\n')}\n)`
			: `worker := varmq.${ctor}(${FUNC[ctor]})`;
		codeEl.innerHTML = highlightGo(head + call);
	};

	const row = (label: string, value: string, tone = '') =>
		`<div><dt>${label}</dt><dd${tone ? ` data-tone="${tone}"` : ''}>${value}</dd></div>`;

	const resolve = () => {
		const rows: string[] = [];
		const conc = Number(val('concurrency'));
		const named = on('name') && val('name') !== '';

		let c: string;
		if (on('concurrency')) c = conc < 1 ? 'one per logical CPU, <code>runtime.NumCPU()</code>' : String(conc);
		else c = on('defaults') ? '8, from <code>DefaultConcurrency</code>' : '1, the default';
		rows.push(row('concurrency', c, on('concurrency') && conc < 1 ? 'warn' : ''));

		rows.push(row('name', named ? `"${val('name')}", added to <code>WorkerRegistry</code> when it starts` : 'none, so it is not in the registry', named ? '' : 'dim'));
		rows.push(row('after the call', on('autoRun') ? '<code>Initiated</code>. It runs nothing until you call <code>Start()</code>.' : '<code>Idle</code>. It started by itself.'));

		let limit = on('concurrency') ? (conc < 1 ? NaN : conc) : on('defaults') ? 8 : 1;
		let idle: string;
		if (on('ratio')) {
			const r = Math.min(100, Math.max(1, Number(val('ratio'))));
			const clamped = r !== Number(val('ratio'));
			idle = Number.isNaN(limit) ? `max(limit × ${r} ÷ 100, 1)` : `max(${limit} × ${r} ÷ 100, 1) = ${Math.max(Math.floor((limit * r) / 100), 1)}`;
			if (clamped) idle += `. ${val('ratio')} is clamped to ${r}.`;
		} else idle = '1, the default';
		rows.push(row('idle goroutines', idle));

		rows.push(
			row(
				'idle timer',
				on('expiry') ? `every ${val('expiry')} s it removes goroutines idle for longer than that, down to the idle minimum` : 'none: the pool shrinks as goroutines finish',
				on('expiry') ? '' : 'dim',
			),
		);
		rows.push(
			row(
				'strategy',
				on('strategy') ? `<code>${val('strategy')}</code>. It matters when more than one queue or topic is bound.` : '<code>Priority</code>, the default',
				on('strategy') ? '' : 'dim',
			),
		);
		rows.push(row('error handler', on('errHandler') ? 'called for every error the worker reports' : 'does nothing. <code>Errs()</code> still holds one error.', on('errHandler') ? '' : 'dim'));
		rows.push(row('context', on('context') ? 'the worker shuts down when <code>ctx</code> is done' : '<code>context.Background()</code>: only <code>Stop</code> cancels it', on('context') ? '' : 'dim'));
		if (on('retention')) {
			rows.push(
				row(
					'stopped retention',
					named
						? `a stopped worker stays in the registry for ${Number(val('retention')) === 0 ? 'good' : `${val('retention')} min`}`
						: 'no effect: only a named worker is registered',
					named ? '' : 'warn',
				),
			);
		}
		rows.push(row('job IDs', on('idGen') ? '<code>Add</code> calls the generator for every job without an ID' : 'none, unless you pass <code>WithJobId</code>', on('idGen') ? '' : 'dim'));
		resEl.innerHTML = rows.join('');
	};

	const update = () => {
		for (const o of opts) o.closest<HTMLElement>('[data-row]')!.dataset.off = String(!o.checked);
		build();
		resolve();
	};

	root.addEventListener('input', update);
	root.addEventListener('change', (e) => {
		const t = e.target as HTMLInputElement;
		if (t.dataset.opt) {
			say(t.checked ? `// ${t.dataset.opt} is in the call now` : `// ${t.dataset.opt} is out of the call: the default applies`);
		}
		update();
	});
	tabs(qs(root, '[data-ctor]'), (v) => {
		ctor = v as Ctor;
		say(`varmq.${ctor}(...) // the options are the same for all three`);
		update();
	});
	root.addEventListener('click', (e) => {
		if ((e.target as HTMLElement).closest('[data-act="clear"]')) {
			for (const o of opts) o.checked = false;
			say('// no options: every default');
			update();
		}
	});

	// Start with a few options on so the call reads as a real one.
	for (const k of ['concurrency', 'name']) qs<HTMLInputElement>(root, `[data-opt="${k}"]`).checked = true;
	update();
}
