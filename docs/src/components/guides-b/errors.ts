// Errors scene: where a failed job is reported, for the three worker types.
// Handle: Err() or Result(). Channel: Errs(), room for one. Handler: WithErrHandler.

import { WorkerSim } from './sim';
import { highlightGo } from './go';
import { announcer, caller, clock, esc, qs, radios, reducedMotion, setText } from './scene';

type Type = 'plain' | 'err' | 'result';
type Outcome = 'ok' | 'error' | 'panic';

const NAME: Record<Type, string> = { plain: 'worker', err: 'err-worker', result: 'result-worker' };
const CTOR: Record<Type, string> = { plain: 'NewWorker', err: 'NewErrWorker', result: 'NewResultWorker' };

document.querySelectorAll<HTMLElement>('[data-scene="errors"]').forEach(init);

function init(root: HTMLElement) {
	let type: Type = 'err';
	let outcome: Outcome = 'error';
	let reader = false;
	let sim = make();
	let slot: string | null = null;
	let dropped = 0;
	let handles: Array<{ id: number; state: 'run' | 'ok' | 'fail'; text: string }> = [];
	let handler: string[] = [];
	const kinds = new Map<number, Outcome>();

	const say = caller(root);
	const live = announcer(root);
	const codeEl = qs(root, '.gb-gocode code');
	const stat = (k: string) => qs(root, `[data-stat="${k}"]`);
	const handlesEl = qs(root, '[data-handles]');
	const handlerEl = qs(root, '[data-handler]');
	const slotEl = qs(root, '[data-slot]');
	const dropsEl = qs(root, '[data-drops]');
	const noteEl = qs(root, '[data-reader-note]');
	const handleName = qs(root, '[data-handle-name]');
	const readerBtn = qs<HTMLButtonElement>(root, '[data-act="reader"]');
	const typeRadios = radios(qs(root, '[data-type]'), (v) => {
		type = v as Type;
		const errBtn = qs<HTMLButtonElement>(root, '[data-outcome] [data-value="error"]');
		errBtn.disabled = type === 'plain';
		errBtn.title = type === 'plain' ? 'A plain worker function returns nothing, so it cannot return an error.' : '';
		if (type === 'plain' && outcome === 'error') {
			outcome = 'panic';
			outcomeRadios.set('panic');
		}
		reset();
		say(`varmq.${CTOR[type]}(...) // a new worker, counters at 0`);
	});
	const outcomeRadios = radios(qs(root, '[data-outcome]'), (v) => {
		outcome = v as Outcome;
		code();
		say(
			outcome === 'ok' ? '// the function returns normally' : outcome === 'error' ? '// the function returns an error' : '// the function panics: the worker recovers it',
		);
	});

	function make() {
		const s = new WorkerSim({ limit: 2 });
		s.onEvent = (e) => {
			if (e.type !== 'end') return;
			ended(e.job.id, !!e.job.fail);
		};
		return s;
	}

	const errText = (id: number, o: Outcome) => (o === 'panic' ? `panic recovered inside ${NAME[type]}: boom` : `job ${id} failed`);

	/** The worker's sendError: the channel takes one, and the handler runs for every failure. */
	const sendError = (text: string) => {
		if (slot === null) slot = text;
		else dropped++;
		handler.unshift(`mailer: ${text}`);
		if (handler.length > 6) handler.pop();
	};

	function ended(id: number, failed: boolean) {
		const o = kinds.get(id) ?? 'ok';
		const h = handles.find((x) => x.id === id);
		if (!h) return;
		if (type === 'err') {
			h.state = failed ? 'fail' : 'ok';
			h.text = failed ? `Err() → ${errText(id, o)}` : 'Err() → nil';
		} else if (type === 'result') {
			h.state = failed ? 'fail' : 'ok';
			// The result is sent only when the function returned no error.
			h.text = failed ? `Result() → (0, ${errText(id, o)})` : `Result() → (${id * 2}, nil)`;
		} else {
			h.state = failed ? 'fail' : 'ok';
			h.text = failed ? 'no handle: the panic went to Errs() and the handler' : 'no handle';
		}
		if (failed) {
			sendError(errText(id, o));
			live(`Job ${id} failed: ${errText(id, o)}. ${slot === errText(id, o) ? 'It is in the Errs channel.' : 'The Errs channel was full, so it was dropped.'}`);
		}
	}

	const body = () => {
		if (outcome === 'panic') return '\tpanic("boom")';
		if (type === 'plain') return '\t_ = j.Data() // do the work, then return';
		if (type === 'err') return outcome === 'ok' ? '\treturn nil' : '\treturn fmt.Errorf("job %d failed", j.Data())';
		return outcome === 'ok' ? '\treturn j.Data() * 2, nil' : '\treturn j.Data() * 2, fmt.Errorf("job %d failed", j.Data())';
	};

	function code() {
		const sig =
			type === 'plain' ? 'func(j varmq.Job[int])' : type === 'err' ? 'func(j varmq.Job[int]) error' : 'func(j varmq.Job[int]) (int, error)';
		const use =
			type === 'plain'
				? 'queue.Add(1) // true: no handle'
				: type === 'err'
					? 'job, _ := queue.Add(1)\nerr := job.Err()'
					: 'job, _ := queue.Add(1)\nres, err := job.Result()';
		codeEl.innerHTML = highlightGo(
			`worker := varmq.${CTOR[type]}(${sig} {\n${body()}\n}, "mailer", varmq.WithErrHandler(func(w varmq.Worker, err error) {\n\tlog.Printf("%s: %v", w.Name(), err)\n}))\nqueue := worker.BindQueue()\n${use}`,
		);
	}

	const addJob = () => {
		const job = sim.add({ dur: 1400 + (sim.now % 7) * 90 + handles.length * 130, fail: outcome === 'ok' ? undefined : outcome });
		kinds.set(job.id, outcome);
		handles.push({ id: job.id, state: 'run', text: type === 'plain' ? 'running' : `${type === 'err' ? 'Err()' : 'Result()'} → blocked until it ends` });
		if (handles.length > 6) handles.shift();
		return job;
	};

	const render = () => {
		const m = sim.m;
		setText(stat('submitted'), String(m.submitted));
		setText(stat('completed'), String(m.completed));
		setText(stat('successful'), String(m.successful));
		setText(stat('failed'), String(m.failed));
		setText(handleName, type === 'plain' ? 'none' : type === 'err' ? 'Err()' : 'Result()');
		handlesEl.innerHTML = handles
			.map((h) => `<li data-state="${h.state}"><span class="gb-job" data-state="${h.state}">${h.id}</span><span>${esc(h.text)}</span></li>`)
			.join('');
		handlerEl.innerHTML = handler.length ? handler.map((l) => `<li class="is-err"><span>${esc(l)}</span></li>`).join('') : '<li class="is-err"><span style="color:var(--panel-ink-2)">not called yet</span></li>';
		slotEl.textContent = slot ?? 'empty';
		slotEl.dataset.full = String(slot !== null);
		setText(dropsEl, dropped ? `dropped: ${dropped}, the channel was full` : 'dropped: 0');
		dropsEl.dataset.tone = dropped ? 'warn' : '';
		setText(noteEl, reader ? 'A goroutine reads the channel.' : 'Nothing reads the channel.');
	};

	function reset() {
		sim = make();
		slot = null;
		dropped = 0;
		handles = [];
		handler = [];
		kinds.clear();
		code();
		render();
	}

	const readerTick = () => {
		if (reader && slot !== null) {
			say(`log.Println("worker error:", err) // ${slot}`);
			slot = null;
		}
	};

	const clk = clock(root, (dt) => {
		sim.advance(dt);
		readerTick();
		render();
	});

	root.addEventListener('click', (e) => {
		const btn = (e.target as HTMLElement).closest<HTMLElement>('[data-act]');
		if (!btn) return;
		const act = btn.dataset.act;
		if (act === 'add' || act === 'add3') {
			const n = act === 'add' ? 1 : 3;
			for (let i = 0; i < n; i++) addJob();
			say(type === 'plain' ? 'queue.Add(n) // true' : `queue.Add(n) // returns a handle and true${n > 1 ? ', 3 times' : ''}`);
			if (reducedMotion()) {
				sim.advance(20000);
				readerTick();
			}
		} else if (act === 'read') {
			if (slot !== null) {
				say(`err := <-worker.Errs() // ${slot}`);
				live(`Read from Errs: ${slot}`);
				slot = null;
			} else say('select { case err := <-worker.Errs(): ... default: } // the channel is empty');
		} else if (act === 'reader') {
			reader = !reader;
			readerBtn.setAttribute('aria-pressed', String(reader));
			readerBtn.textContent = `Reader goroutine: ${reader ? 'on' : 'off'}`;
			say(reader ? 'go func() { for err := range worker.Errs() { ... } }()' : '// no reader: the channel keeps the first error');
			readerTick();
		}
		render();
		clk.wake();
	});

	typeRadios.set('err');
	code();
	render();
}
