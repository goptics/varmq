// The figures of the home page's benchmark section, JOB 04 (components/home/Race.astro).
// They are the explorer's own means (laneSet over a scope of data.json), so the home page and the
// Benchmarks page cannot disagree. home.test.ts pins them to the values the page states.

import { defaultState, isVarmq, laneSet, leaders, metricsFor, normalize, ranked, scopeOf } from './lib.ts';
import type { Cell, Data, Dataset, Filters, Lane, Metric, State } from './lib.ts';

export interface Chart {
	id: string;
	title: string;
	metric: Metric;
	/** Groups averaged into each lane. */
	groups: number;
	/** One lane per library, best first. VarMQ's second subject (distributed, single file) is left out. */
	lanes: Lane[];
	/** Subjects missing from some group of the scope, so not in the lanes. */
	partial: string[];
}

const stateOf = (data: Data, view: string, f: Filters, patch: Partial<State> = {}): State => {
	const s = defaultState(data, view);
	return normalize(data, { ...s, ...patch, f: { ...s.f, ...f } });
};

function chart(data: Data, id: string, title: string, s: State, metric = 'rate'): Chart {
	const st = { ...s, metric };
	const m = metricsFor(viewSuite(s.view), st.f).find((x) => x.id === metric)!;
	const set = laneSet(scopeOf(data, st), m, st);
	return {
		id,
		title,
		metric: m,
		groups: set.groups,
		lanes: set.lanes.filter((l) => l.subject === 'VarMQ' || !isVarmq(l.subject)),
		partial: set.partial.map((p) => p.subject),
	};
}

const viewSuite = (view: string) => (view.startsWith('pool') ? 'pool' : (view as 'redis' | 'sqlite'));

// The scopes. Pool: the 50K-worker groups of the three pool files. Redis: no-op jobs, one instance, 100 workers.
// SQLite: draining one queue with a WAL checkpoint every 1,000 pages.
export const POOL: Filters = { workers: '50K' };
export const REDIS: Filters = { test: 'durable', task: 'Noop', workers: '100', instances: '1' };
export const SQLITE: Filters = { scenario: 'Drain1Q', ckpt: '1000' };

export function homeCharts(data: Data) {
	const pool = stateOf(data, 'pool-all', POOL);
	const redis = (shape: string) => stateOf(data, 'redis', { ...REDIS, shape });
	return {
		pool: [chart(data, 'pool-rate', 'Throughput', pool), chart(data, 'pool-memory', 'Memory', pool, 'memory')],
		redis: [chart(data, 'redis-many', '100 producers, 1,000 jobs each', redis('100u-1Kt')), chart(data, 'redis-one', 'One producer, 100,000 jobs', redis('1u-100Kt'))],
		sqlite: [chart(data, 'sqlite-drain', 'Drain one queue', stateOf(data, 'sqlite', SQLITE))],
	};
}

// ---- results against VarMQ, for the callout ----

const ds = (data: Data, id: string) => data.datasets.find((d) => d.id === id)!;

// ranked() reads only `baselines` from the state: the channel baselines count, as on the Benchmarks page.
const withBaselines = { baselines: true } as State;

/** The subject alone at the top of a group on throughput, or null for a tie. */
function leader(d: Dataset, g: Cell['g']): string | null {
	const p = ranked({ ds: d, g }, metricsFor(d.suite, g.key)[0], withBaselines);
	const top = leaders(p);
	return top.length === 1 ? top[0].subject : null;
}

const count = (d: Dataset, keep: (g: Cell['g']) => boolean, subject: string) => d.groups.filter((g) => keep(g) && leader(d, g) === subject).length;

export function againstVarmq(data: Data) {
	const { pool } = homeCharts(data);
	const mem = pool[1].lanes;
	const varmqMB = mem.find((l) => l.subject === 'VarMQ')!.value;
	const antsMB = mem.find((l) => l.subject === 'Ants')!.value;

	const pools = data.datasets.filter((d) => d.suite === 'pool');
	const oneWorker = (g: Cell['g']) => g.key.task === 'Loop10' && g.key.workers === '1';
	const all = () => true;
	const byCpu = (procs: number) => {
		const d = pools.find((x) => x.procs === procs)!;
		return { groups: d.groups.length, varmq: count(d, all, 'VarMQ'), buffered: count(d, all, 'BufferedPool') };
	};

	const sqlite = ds(data, 'sqlite');
	const redis = ds(data, 'redis');
	// The rows where one producer enqueues 100,000 jobs: every drain test, and the enqueue-only test.
	const oneProducer = (g: Cell['g']) => g.key.shape === '1u-100Kt';
	// The 100-producer rows on one instance, by worker count: VarMQ leads all of them at these counts.
	const manyC1 = (w: string) => (g: Cell['g']) => g.key.test === 'durable' && g.key.shape === '100u-1Kt' && g.key.instances === '1' && g.key.workers === w;
	const workers = [...new Set(redis.groups.filter((g) => g.key.test === 'durable').map((g) => g.key.workers))];

	return {
		memory: { varmq: varmqMB, ants: antsMB, ratio: varmqMB / antsMB },
		oneWorker: {
			groups: pools.reduce((n, d) => n + d.groups.filter(oneWorker).length, 0),
			buffered: pools.reduce((n, d) => n + count(d, oneWorker, 'BufferedPool'), 0),
		},
		cpu4: byCpu(4),
		cpu24: byCpu(24),
		redisOne: {
			groups: redis.groups.filter(oneProducer).length,
			rmq: count(redis, oneProducer, 'Rmq'),
			varmqEnqueue: count(redis, (g) => oneProducer(g) && g.key.test === 'enqueue', 'VarMQ') === 1,
		},
		sqlite: { groups: sqlite.groups.length, varmq: count(sqlite, all, 'VarMQ') },
		redisManyWorkers: workers.filter((w) => {
			const gs = redis.groups.filter(manyC1(w));
			return gs.length > 0 && gs.every((g) => leader(redis, g) === 'VarMQ');
		}),
	};
}

/** Median enqueue-to-handler latency (ms) of a Redis subject. */
export function latencyP50(data: Data, subject: string): { value: number; lowest: boolean } {
	const d = ds(data, 'redis');
	const g = d.groups.find((x) => x.key.test === 'latency')!;
	const m = metricsFor('redis', g.key).find((x) => x.id === 'p50')!;
	const p = ranked({ ds: d, g }, m, withBaselines);
	return { value: p.find((r) => r.subject === subject)!.value, lowest: p[0].subject === subject };
}
