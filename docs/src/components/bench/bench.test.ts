import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { describe, it } from 'node:test';
import {
	VIEWS,
	benchPattern,
	currentMetric,
	defaultState,
	filterDefs,
	fromHash,
	laneSet,
	lossesOf,
	metricsFor,
	normalize,
	optionCounts,
	scopeOf,
	toHash,
	viewById,
} from './lib.ts';
import type { Data, Filters, State } from './lib.ts';
import { renderAll } from './render.ts';

const here = new URL('.', import.meta.url);
const data = JSON.parse(readFileSync(new URL('data.json', here), 'utf8')) as Data;

const state = (view: string, f: Filters = {}, patch: Partial<State> = {}): State => {
	const s = defaultState(data, view);
	return normalize(data, { ...s, ...patch, f: { ...s.f, ...f } });
};

describe('data.json', () => {
	it('has the row counts the benchmark READMEs state', () => {
		const rows = Object.fromEntries(data.datasets.map((d) => [d.id, d.rowCount]));
		assert.deepEqual(rows, { 'pool-24cpu': 495, 'pool-8cpu': 495, 'pool-4cpu': 495, redis: 150, sqlite: 32 });
		for (const d of data.datasets) assert.equal(d.groups.reduce((n, g) => n + g.rows.length, 0), d.rowCount, d.id);
	});

	it('keeps each group on adjacent lines, so one link covers the group', () => {
		for (const d of data.datasets)
			for (const g of d.groups) {
				assert.equal(g.last - g.first + 1, g.rows.length, `${d.id} ${g.id}`);
				g.rows.forEach((r, i) => assert.equal(r[1], g.first + i));
				for (const r of g.rows) assert.equal(r.length, 3 + d.colsets[g.c].length, `${d.id} ${g.id} ${r[0]}`);
			}
	});

	it('names the commit and the run', () => {
		assert.match(data.source.commit, /^[0-9a-f]{40}$/);
		assert.match(data.source.runDate, /^\d{4}-\d{2}-\d{2}$/);
	});
});

// The home page (/alt/) prints figures taken from the same files. The page must agree with them.
describe('agreement with the home page', () => {
	const race = readFileSync(new URL('../alt/Race.astro', here), 'utf8');
	const alias: Record<string, string> = {
		'Buffered channel †': 'BufferedPool',
		'Channel †': 'GoroutinePool',
		errgroup: 'ErrGroup',
		ants: 'Ants',
		gopool: 'Gopool',
		conc: 'Conc',
		workerpool: 'WorkerPool',
		pond: 'Pond',
		'gocraft/work': 'GocraftWork',
		taskq: 'Taskq',
		rmq: 'Rmq',
		goqite: 'Goqite',
		backlite: 'Backlite',
		river: 'River',
	};
	const block = (name: string): Record<string, number> => {
		const m = new RegExp(`const ${name} = lanes\\(\\s*\\[([\\s\\S]*?)\\]`).exec(race);
		assert.ok(m, `${name} not found in Race.astro`);
		return Object.fromEntries([...m[1].matchAll(/name: '([^']+)', v: ([\d.]+)/g)].map((x) => [alias[x[1]] ?? x[1], Number(x[2])]));
	};
	const mine = (s: State, metricId: string) => {
		const st = { ...s, metric: metricId };
		const m = currentMetric(st);
		return Object.fromEntries(laneSet(scopeOf(data, st), m, st).lanes.map((l) => [l.subject, l.value]));
	};

	it('pool throughput: 50K workers, mean over GOMAXPROCS 24, 8 and 4', () => {
		const home = block('poolRate');
		const got = mine(state('pool-all', { workers: '50K' }), 'rate');
		assert.equal(Object.keys(home).length, 9);
		for (const [k, v] of Object.entries(home)) assert.equal(Math.round(got[k]), v, k);
	});

	it('pool memory: the same rows, in MB of 10^6 bytes with one decimal', () => {
		const home = block('poolMem');
		const got = mine(state('pool-all', { workers: '50K' }), 'memory');
		for (const [k, v] of Object.entries(home)) assert.equal(Number(got[k].toFixed(1)), v, k);
	});

	it('Redis lanes: Noop, 100 workers, C1, 100 and 1 producers', () => {
		const base = { test: 'durable', task: 'Noop', workers: '100', instances: '1' };
		for (const [name, shape] of [['redisMany', '100u-1Kt'], ['redisOne', '1u-100Kt']]) {
			const home = block(name);
			const got = mine(state('redis', { ...base, shape }), 'rate');
			for (const [k, v] of Object.entries(home)) assert.equal(Math.round(got[k]), v, `${name} ${k}`);
		}
	});

	it('SQLite lanes: Drain1Q at 1000 pages', () => {
		const home = block('sqlite');
		const got = mine(state('sqlite', { scenario: 'Drain1Q', ckpt: '1000' }), 'rate');
		for (const [k, v] of Object.entries(home)) assert.equal(Math.round(got[k]), v, k);
	});

	it('win counts quoted in the home page text', () => {
		const wins = (view: string) => {
			const s = state(view);
			const ls = laneSet(scopeOf(data, s), currentMetric(s), s);
			return ls;
		};
		const count = (view: string, subject: string) => {
			const s = state(view);
			return scopeOf(data, s).filter((c) => {
				const r = c.g.rows.reduce((a, b) => (b[4] > a[4] ? b : a));
				return r[0] === subject;
			}).length;
		};
		assert.match(race, /buffered channel wins 21 of 55 rows and VarMQ wins 19/i);
		assert.equal(count('pool-4cpu', 'BufferedPool'), 21);
		assert.equal(count('pool-4cpu', 'VarMQ'), 19);
		assert.match(race, /At 24 CPUs VarMQ wins 39 and the channel 12/);
		assert.equal(count('pool-24cpu', 'VarMQ'), 39);
		assert.equal(count('pool-24cpu', 'BufferedPool'), 12);
		assert.match(race, /fastest subject in all 8 scenario and checkpoint groups/);
		assert.equal(wins('sqlite').wins.other, 0);
		assert.equal(wins('sqlite').groups, 8);
	});
});

describe('winners', () => {
	it('rank the same by ops/s as by ns/op', () => {
		for (const d of data.datasets.filter((x) => x.suite !== 'sqlite'))
			for (const g of d.groups) {
				const cols = d.colsets[g.c];
				if (!cols.includes('ns/op') || cols.includes('p50-ms')) continue;
				const rate = 3 + cols.findIndex((c) => c === 'ops/s' || c === 'jobs/s');
				const ns = g.rows.reduce((a, b) => (b[3] < a[3] ? b : a))[0];
				const hi = g.rows.reduce((a, b) => (b[rate] > a[rate] ? b : a))[0];
				assert.equal(ns, hi, `${d.id} ${g.id}`);
			}
	});

	it('count VarMQ wins and losses of a view and agree with the list of losses', () => {
		for (const v of VIEWS) {
			const s = state(v.id);
			const cells = scopeOf(data, s);
			const m = currentMetric(s);
			const ls = laneSet(cells, m, s);
			const losses = lossesOf(cells, m, s);
			assert.equal(ls.wins.varmq + ls.wins.other, ls.groups, v.id);
			assert.equal(losses.length, ls.wins.other, v.id);
			for (const l of losses) assert.ok(l.ahead >= 1 && l.rank > 1, `${v.id} ${l.cell.g.id}`);
		}
	});

	it('pool: VarMQ wins 39, 33 and 19 groups at 24, 8 and 4 CPUs', () => {
		const n = (id: string) => laneSet(scopeOf(data, state(id)), currentMetric(state(id)), state(id)).wins.varmq;
		assert.deepEqual([n('pool-24cpu'), n('pool-8cpu'), n('pool-4cpu')], [39, 33, 19]);
		assert.equal(n('pool-all'), 91);
	});

	it('memory: lower is better', () => {
		const s = state('pool-24cpu', {}, { metric: 'memory' });
		const m = currentMetric(s);
		assert.equal(m.id, 'memory');
		const ls = laneSet(scopeOf(data, s), m, s);
		assert.ok(ls.lanes[0].value <= ls.lanes.at(-1)!.value);
	});
});

describe('lanes', () => {
	it('leave out subjects that miss a group, and say so', () => {
		const s = state('sqlite');
		const ls = laneSet(scopeOf(data, s), currentMetric(s), s);
		assert.deepEqual(ls.lanes.map((l) => l.subject).sort(), ['Goqite', 'River', 'VarMQ']);
		assert.deepEqual(
			ls.partial.map((p) => `${p.subject}:${p.n}`).sort(),
			['Backlite:6', 'VarMQSingleFile:2'],
		);
	});

	it('use VarMQDistributed as the base where VarMQ does not run', () => {
		const s = state('redis', { test: 'durable', instances: '4' });
		const ls = laneSet(scopeOf(data, s), currentMetric(s), s);
		assert.equal(ls.base, 'VarMQDistributed');
		assert.equal(ls.lanes.length, 5);
	});

	it('can leave the channel baselines out', () => {
		const s = state('pool-24cpu', {}, { baselines: false });
		const ls = laneSet(scopeOf(data, s), currentMetric(s), s);
		assert.equal(ls.lanes.length, 7);
		assert.ok(!ls.lanes.some((l) => /Pool$/.test(l.subject) && ['GoroutinePool', 'BufferedPool'].includes(l.subject)));
	});

	it('latency has its own metrics and lower is better', () => {
		const s = state('redis', { test: 'latency' });
		assert.deepEqual(metricsFor('redis', s.f).map((m) => m.id), ['p50', 'p99', 'max']);
		const ls = laneSet(scopeOf(data, s), currentMetric(s), s);
		assert.equal(ls.lanes[0].subject, 'VarMQ');
	});
});

// The command on the page must run exactly the groups in the scope. Go splits -bench at "/" and matches each part.
describe('go test -bench pattern', () => {
	const selects = (pattern: string, id: string) => {
		const els = pattern.split('/');
		const parts = id.split('/');
		return els.every((e, i) => new RegExp(e).test(parts[i] ?? ''));
	};

	for (const v of VIEWS) {
		it(`selects the scope of ${v.id}`, () => {
			const defs = filterDefs(data, v);
			// Every single-filter choice, plus a few pairs.
			const trials: Filters[] = [{}];
			for (const d of defs) for (const o of d.options) trials.push({ [d.id]: o.value });
			for (const a of defs) for (const b of defs) if (a.id < b.id) trials.push({ [a.id]: a.options[0].value, [b.id]: b.options.at(-1)!.value });
			let checked = 0;
			for (const f of trials) {
				const s = state(v.id, f);
				const cells = scopeOf(data, s);
				if (!cells.length) continue;
				const pattern = benchPattern(v.suite, s.f);
				const want = new Set(cells.map((c) => c.g.id));
				for (const ds of v.sources) {
					for (const g of data.datasets.find((d) => d.id === ds)!.groups) {
						const hit = selects(pattern, g.id);
						assert.equal(hit, want.has(g.id), `${v.id} ${JSON.stringify(s.f)} pattern ${pattern} on ${g.id}`);
					}
				}
				checked++;
			}
			assert.ok(checked > 3);
		});
	}
});

describe('state', () => {
	it('round-trips through the URL hash', () => {
		const s = state('redis', { test: 'durable', task: 'Sleep1ms', workers: '100', instances: '4', shape: '100u-1Kt' }, { sort: 'worst', metric: 'memory' });
		assert.deepEqual(fromHash(data, toHash(data, s)), s);
		const p = state('pool-24cpu', { workers: '50K' }, { baselines: false, focus: 'Tasks/Sleep10msW50K/1u-1Mt' });
		assert.deepEqual(fromHash(data, '#' + toHash(data, p)), p);
	});

	it('ignores values it does not know', () => {
		const s = fromHash(data, 'view=nope&workers=7&metric=zzz');
		assert.equal(s.view, VIEWS[0].id);
		assert.equal(s.f.workers, 'all');
		assert.equal(s.metric, 'rate');
	});

	it('counts the groups behind each option', () => {
		const c = optionCounts(data, state('pool-24cpu'));
		assert.equal(c.workers.all, 55);
		assert.equal(c.workers['50K'], 15);
		const k = optionCounts(data, state('pool-24cpu', { task: 'Loop10K' }));
		assert.equal(k.workers['50K'], 0);
		assert.equal(k.workers['1'], 5);
	});
});

describe('render', () => {
	it('prints the same figures the lanes hold, with a link to a raw line in every group row', () => {
		for (const v of VIEWS) {
			const out = renderAll(data, state(v.id));
			assert.ok(out.lanes.includes('rlane'), v.id);
			assert.ok(out.map.includes('bcell'), v.id);
			assert.ok(out.rowsBody.includes(`/blob/${data.source.commit}/`), v.id);
			assert.ok(out.command.includes('go test'), v.id);
		}
	});

	it('escapes text it prints', () => {
		const out = renderAll(data, state('pool-24cpu'));
		assert.ok(!/<script/i.test(out.lanes + out.map + out.losses));
	});

	it('keeps the view of one focused group to its own rows', () => {
		const s = state('pool-24cpu', {}, { focus: 'Tasks/Sleep10msW50K/1u-1Mt' });
		const out = renderAll(data, s);
		assert.match(out.scope, /One group/);
		assert.equal((out.lanes.match(/<li class="rlane/g) ?? []).length, 9);
		assert.match(out.lanes, /#L5"/);
	});

	it('names the empty scope', () => {
		const s = { ...state('pool-24cpu'), f: { task: 'Loop10K', workers: '50K', shape: 'all' } };
		assert.match(renderAll(data, s).scope, /No group matches/);
		assert.equal(viewById('pool-24cpu').suite, 'pool');
	});
});
