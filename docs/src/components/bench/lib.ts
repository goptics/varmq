// Pure benchmark logic, shared by the build (Explorer.astro) and the browser (explorer.ts).
// Every number comes from data.json, which scripts/bench-data.mjs copies from the raw `go test -bench` files.
// Nothing here knows about the DOM, so the tests run it under plain Node.

export type Suite = 'pool' | 'redis' | 'sqlite';
export type Kind = 'varmq' | 'lib' | 'baseline';

/** [subject, line in the raw file, iterations, ...one value per column of the group's column set]. */
export type Row = [subject: string, line: number, iterations: number, ...values: number[]];

export interface Group {
	id: string;
	key: Record<string, string>;
	c: number;
	first: number;
	last: number;
	rows: Row[];
}

export interface Dataset {
	id: string;
	suite: Suite;
	title: string;
	file: string;
	procs: number;
	cpu: string;
	goos: string;
	goarch: string;
	pkg: string;
	seconds: number;
	rowCount: number;
	runDate: string;
	readme: string;
	colsets: string[][];
	groups: Group[];
}

export interface Version {
	name: string;
	label: string;
	url: string | null;
	version: string;
	suites: string[];
}

export interface Data {
	schema: number;
	source: { repo: string; commit: string; commitDate: string; runDate: string };
	env: { machine: string; pinning: string; versions: Version[] };
	datasets: Dataset[];
}

/** One group of rows from one dataset. */
export interface Cell {
	ds: Dataset;
	g: Group;
}

// ---- views: the five published datasets, plus the three pool files read together ----

export interface View {
	id: string;
	tab: string;
	title: string;
	suite: Suite;
	sources: string[];
	/** The chart id on benchmarks.goptics.org, when the view is one published dataset. */
	chart: string | null;
	blurb: string;
}

export const VIEWS: View[] = [
	{
		id: 'pool-all',
		tab: 'All pools',
		title: 'Worker pool, GOMAXPROCS 24, 8 and 4',
		suite: 'pool',
		sources: ['pool-24cpu', 'pool-8cpu', 'pool-4cpu'],
		chart: null,
		blurb: 'The three pool files read together. A mean here averages the 24, 8 and 4 CPU runs.',
	},
	{ id: 'pool-24cpu', tab: '24 CPUs', title: 'Worker pool, GOMAXPROCS 24', suite: 'pool', sources: ['pool-24cpu'], chart: 'pool-24cpu', blurb: 'Nine pools, 1M tasks per run.' },
	{ id: 'pool-8cpu', tab: '8 CPUs', title: 'Worker pool, GOMAXPROCS 8', suite: 'pool', sources: ['pool-8cpu'], chart: 'pool-8cpu', blurb: 'The same matrix with GOMAXPROCS 8.' },
	{ id: 'pool-4cpu', tab: '4 CPUs', title: 'Worker pool, GOMAXPROCS 4', suite: 'pool', sources: ['pool-4cpu'], chart: 'pool-4cpu', blurb: 'The same matrix with GOMAXPROCS 4.' },
	{ id: 'redis', tab: 'Redis', title: 'Message queues on Redis', suite: 'redis', sources: ['redis'], chart: 'redis', blurb: 'Six subjects from five libraries on Redis 7.2.' },
	{ id: 'sqlite', tab: 'SQLite', title: 'Message queues on SQLite', suite: 'sqlite', sources: ['sqlite'], chart: 'sqlite', blurb: 'Five subjects from four libraries, WAL checkpoint at 1000 and 10000 pages.' },
];

export const viewById = (id: string) => VIEWS.find((v) => v.id === id) ?? VIEWS[0];

// ---- subjects ----

export interface SubjectInfo {
	code: string;
	kind: Kind;
	note: string;
}

// Display metadata only. The names are the benchmark names, so a reader can search the raw files for them.
export const SUBJECTS: Record<string, SubjectInfo> = {
	VarMQ: { code: 'VQ', kind: 'varmq', note: 'varmq. Pool: a worker bound to an in-memory queue. Redis: varmq with redisq, one instance. SQLite: varmq with sqliteq, one file per queue.' },
	VarMQDistributed: { code: 'VD', kind: 'varmq', note: 'varmq with redisq as a distributed queue.' },
	VarMQSingleFile: { code: 'VS', kind: 'varmq', note: 'varmq with sqliteq, every queue in one database file. Runs in Mixed10Q only.' },
	Ants: { code: 'AN', kind: 'lib', note: 'panjf2000/ants, default options. Submit blocks until a worker is free.' },
	WorkerPool: { code: 'WP', kind: 'lib', note: 'gammazero/workerpool. Submit never blocks.' },
	Conc: { code: 'CO', kind: 'lib', note: 'sourcegraph/conc pool with a goroutine cap.' },
	ErrGroup: { code: 'EG', kind: 'lib', note: 'x/sync errgroup with SetLimit: one goroutine per task, not a pool.' },
	Gopool: { code: 'GP', kind: 'lib', note: 'bytedance/gopkg gopool. Soft worker cap, unbounded task list.' },
	Pond: { code: 'PO', kind: 'lib', note: 'alitto/pond v2 with an unbounded queue.' },
	GoroutinePool: { code: 'GR', kind: 'baseline', note: 'Baseline, not a library: N goroutines reading one unbuffered channel.' },
	BufferedPool: { code: 'BP', kind: 'baseline', note: 'Baseline, not a library: N goroutines reading a channel that holds all 1M tasks.' },
	Asynq: { code: 'AS', kind: 'lib', note: 'hibiken/asynq.' },
	GocraftWork: { code: 'GW', kind: 'lib', note: 'gocraft/work.' },
	Rmq: { code: 'RM', kind: 'lib', note: 'adjust/rmq.' },
	Taskq: { code: 'TQ', kind: 'lib', note: 'vmihailenco/taskq.' },
	Goqite: { code: 'GQ', kind: 'lib', note: 'maragudk/goqite.' },
	Backlite: { code: 'BL', kind: 'lib', note: 'mikestefanello/backlite.' },
	River: { code: 'RV', kind: 'lib', note: 'riverqueue/river with its SQLite driver.' },
};

const info = (s: string): SubjectInfo => SUBJECTS[s] ?? { code: s.slice(0, 2).toUpperCase(), kind: 'lib', note: '' };
export const kindOf = (s: string) => info(s).kind;
export const isVarmq = (s: string) => info(s).kind === 'varmq';
export const codeOf = (s: string) => info(s).code;
export const noteOf = (s: string) => info(s).note;
export const label = (s: string) => (info(s).kind === 'baseline' ? `${s} †` : s);

// ---- filters ----

export type Filters = Record<string, string>;

export interface FilterDef {
	id: string;
	label: string;
	/** True when "All" is a choice. */
	all: boolean;
	options: { value: string; label: string }[];
	/** The filter has an effect only when this returns true. */
	when?: (f: Filters) => boolean;
}

export function producerLabel(shape: string): string {
	const m = /^(\d+[KM]?)u-(\d+[KM]?)t$/.exec(shape);
	if (!m) return shape;
	return m[1] === '1' ? '1 producer' : `${m[1]} producers`;
}

export const workerLabel = (v: string) => (v === '1' ? '1 worker' : `${v} workers`);

const TEST_LABEL: Record<string, string> = { durable: 'Drain 100K jobs', enqueue: 'Enqueue only', latency: 'Latency' };

function optionsOf(datasets: Dataset[], id: string, keep: (g: Group) => boolean = () => true): string[] {
	const seen: string[] = [];
	for (const d of datasets) for (const g of d.groups) if (keep(g) && id in g.key && !seen.includes(g.key[id])) seen.push(g.key[id]);
	return seen;
}

export function filterDefs(data: Data, view: View): FilterDef[] {
	const ds = view.sources.map((id) => data.datasets.find((d) => d.id === id)!);
	const opts = (id: string, fmt: (v: string) => string = (v) => v, keep?: (g: Group) => boolean) =>
		optionsOf(ds, id, keep).map((value) => ({ value, label: fmt(value) }));
	if (view.suite === 'pool') {
		return [
			{ id: 'task', label: 'Task', all: true, options: opts('task') },
			{ id: 'workers', label: 'Workers', all: true, options: opts('workers', workerLabel) },
			{ id: 'shape', label: 'Producers', all: true, options: opts('shape', producerLabel) },
		];
	}
	if (view.suite === 'redis') {
		const durable = (f: Filters) => f.test === 'durable';
		return [
			{ id: 'test', label: 'Test', all: false, options: opts('test', (v) => TEST_LABEL[v] ?? v) },
			{ id: 'task', label: 'Task', all: true, when: durable, options: opts('task', (v) => (v === 'Sleep1ms' ? 'Sleep 1 ms' : v)) },
			{ id: 'workers', label: 'Workers', all: true, when: durable, options: opts('workers', (v) => `${v}`) },
			{ id: 'instances', label: 'Instances', all: false, when: durable, options: opts('instances', (v) => (v === '1' ? '1 (C1)' : `${v} (C${v})`)) },
			// The latency test has its own shape and no producer filter, so its shape is not an option here.
			{ id: 'shape', label: 'Producers', all: true, when: (f) => f.test !== 'latency', options: opts('shape', producerLabel, (g) => g.key.test !== 'latency') },
		];
	}
	return [
		{ id: 'scenario', label: 'Scenario', all: true, options: opts('scenario') },
		{ id: 'ckpt', label: 'WAL checkpoint', all: true, options: opts('ckpt', (v) => `${v} pages`) },
	];
}

// ---- metrics ----

export interface Metric {
	id: string;
	label: string;
	/** Unit shown beside values. */
	unit: string;
	/** Short lane title. */
	title: string;
	better: 'high' | 'low';
	/** The column of the raw file the value is read from. */
	col: string;
	/** The raw value is divided by this. B/op becomes MB (10^6 bytes). */
	scale: number;
	/** Number of decimals; 'sig' prints four significant digits, like the raw latency columns. */
	dec: number | 'sig';
}

const RATE = (suite: Suite): Metric => ({
	id: 'rate',
	label: 'Throughput',
	unit: suite === 'pool' ? 'tasks/s' : 'jobs/s',
	title: suite === 'pool' ? 'Tasks per second' : 'Jobs per second',
	better: 'high',
	col: suite === 'pool' ? 'ops/s' : 'jobs/s',
	scale: 1,
	dec: 0,
});

export function metricsFor(suite: Suite, f: Filters): Metric[] {
	if (suite === 'redis' && f.test === 'latency') {
		const ms = (id: string, lab: string, col: string): Metric => ({ id, label: lab, unit: 'ms', title: `${lab} latency`, better: 'low', col, scale: 1, dec: 'sig' });
		return [ms('p50', 'Median (p50)', 'p50-ms'), ms('p99', 'p99', 'p99-ms'), ms('max', 'Max', 'max-ms')];
	}
	if (suite === 'sqlite') {
		return [RATE(suite), { id: 'wal', label: 'WAL size', unit: 'MB', title: 'Peak WAL size', better: 'low', col: 'walMB', scale: 1, dec: 1 }];
	}
	return [RATE(suite), { id: 'memory', label: 'Memory', unit: 'MB', title: suite === 'pool' ? 'MB allocated per 1M tasks' : 'MB allocated per run', better: 'low', col: 'B/op', scale: 1e6, dec: 1 }];
}

export function metricValue(ds: Dataset, g: Group, r: Row, m: Metric): number | undefined {
	const i = ds.colsets[g.c].indexOf(m.col);
	return i < 0 ? undefined : r[3 + i] / m.scale;
}

export function fmt(v: number, m: Metric): string {
	if (m.dec === 'sig') return Number(v.toPrecision(4)).toLocaleString('en-US', { maximumFractionDigits: 5 });
	const dec = m.dec === 1 && v < 10 ? 2 : m.dec;
	return v.toLocaleString('en-US', { minimumFractionDigits: dec, maximumFractionDigits: dec });
}

export function fmtRatio(x: number): string {
	return x.toLocaleString('en-US', { minimumFractionDigits: x >= 100 ? 0 : 2, maximumFractionDigits: x >= 100 ? 0 : 2 }) + '×';
}

// ---- state ----

export type Sort = 'best' | 'worst' | 'name';

export interface State {
	view: string;
	f: Filters;
	metric: string;
	sort: Sort;
	/** Pool only: keep the two channel baselines in the lanes and in the win count. */
	baselines: boolean;
	/** One group id, or null for the whole scope. A pool-all focus is "<dataset>|<group>". */
	focus: string | null;
}

export function defaultState(data: Data, viewId: string): State {
	const view = viewById(viewId);
	const f: Filters = {};
	for (const d of filterDefs(data, view)) f[d.id] = d.all ? 'all' : d.options[0].value;
	return { view: view.id, f, metric: 'rate', sort: 'best', baselines: true, focus: null };
}

// ---- scope ----

export function sourcesOf(data: Data, view: View): Dataset[] {
	return view.sources.map((id) => data.datasets.find((d) => d.id === id)!);
}

/** A filter that is hidden has no effect on the scope. */
export function activeFilters(defs: FilterDef[], f: Filters): FilterDef[] {
	return defs.filter((d) => !d.when || d.when(f));
}

export function scopeOf(data: Data, state: State): Cell[] {
	const view = viewById(state.view);
	const defs = activeFilters(filterDefs(data, view), state.f);
	const out: Cell[] = [];
	for (const ds of sourcesOf(data, view)) {
		for (const g of ds.groups) {
			if (view.suite === 'redis' && g.key.test !== state.f.test) continue;
			if (defs.every((d) => state.f[d.id] === 'all' || (state.f[d.id] ?? 'all') === g.key[d.id] || !(d.id in g.key))) out.push({ ds, g });
		}
	}
	return out;
}

export const cellId = (c: Cell, multi: boolean) => (multi ? `${c.ds.id}|${c.g.id}` : c.g.id);

/** Remove filter values that match no group, so the state never points at an empty scope by itself. */
export function normalize(data: Data, state: State): State {
	const view = viewById(state.view);
	const defs = filterDefs(data, view);
	const f = { ...state.f };
	for (const d of defs) {
		const valid = (d.all && f[d.id] === 'all') || d.options.some((o) => o.value === f[d.id]);
		if (!valid) f[d.id] = d.all ? 'all' : d.options[0].value;
	}
	const metrics = metricsFor(view.suite, f);
	const metric = metrics.some((m) => m.id === state.metric) ? state.metric : metrics[0].id;
	return { ...state, view: view.id, f, metric, baselines: view.suite === 'pool' ? state.baselines : true };
}

export function currentMetric(state: State): Metric {
	const ms = metricsFor(viewById(state.view).suite, state.f);
	return ms.find((m) => m.id === state.metric) ?? ms[0];
}

/** For each filter option, whether choosing it (with the other filters as they are) leaves any group. */
export function optionCounts(data: Data, state: State): Record<string, Record<string, number>> {
	const view = viewById(state.view);
	const defs = filterDefs(data, view);
	const out: Record<string, Record<string, number>> = {};
	for (const d of defs) {
		out[d.id] = {};
		const values = [...(d.all ? ['all'] : []), ...d.options.map((o) => o.value)];
		for (const v of values) out[d.id][v] = scopeOf(data, { ...state, f: { ...state.f, [d.id]: v } }).length;
	}
	return out;
}

// ---- rows, winners, lanes ----

export interface Ranked {
	subject: string;
	value: number;
	line: number;
}

/** The rows of one group that have the metric, best first. Ties keep file order. */
export function ranked(c: Cell, m: Metric, state: State): Ranked[] {
	const out: Ranked[] = [];
	for (const r of c.g.rows) {
		if (!state.baselines && kindOf(r[0]) === 'baseline') continue;
		const value = metricValue(c.ds, c.g, r, m);
		if (value !== undefined) out.push({ subject: r[0], value, line: r[1] });
	}
	return out.sort((a, b) => (m.better === 'high' ? b.value - a.value : a.value - b.value));
}

/** The rows that share the best value of a ranked group: one row, or every row of a tie. */
export const leaders = (p: Ranked[]): Ranked[] => p.filter((r) => r.value === p[0].value);

/**
 * Who leads a ranked group. A tie for the best value between a VarMQ subject and another subject is its own
 * outcome, not a lead for either. A tie between two VarMQ subjects is a VarMQ lead, and between two others
 * a lead for another subject.
 */
export type Outcome = 'varmq' | 'other' | 'tie';
export function outcomeOf(p: Ranked[]): Outcome {
	const top = leaders(p);
	const vq = top.filter((r) => isVarmq(r.subject)).length;
	return vq === top.length ? 'varmq' : vq === 0 ? 'other' : 'tie';
}

export interface Lane {
	subject: string;
	value: number;
	ratio: number;
	/** Groups in scope in which this subject alone has the best row. A shared best value is no one's win. */
	won: number;
	of: number;
}

export interface LaneSet {
	lanes: Lane[];
	base: string | null;
	groups: number;
	/** Subjects that miss at least one group of the scope, with the number of groups they have. */
	partial: { subject: string; n: number }[];
	/** Groups by outcome (outcomeOf): led by VarMQ, led by another subject, or tied between them. */
	wins: { varmq: number; other: number; tied: number };
}

export function laneSet(cells: Cell[], m: Metric, state: State): LaneSet {
	const per = cells.map((c) => ranked(c, m, state));
	const groups = per.filter((p) => p.length).length;
	const count = new Map<string, number>();
	const sum = new Map<string, number>();
	const won = new Map<string, number>();
	const wins = { varmq: 0, other: 0, tied: 0 };
	for (const p of per) {
		if (!p.length) continue;
		for (const r of p) {
			count.set(r.subject, (count.get(r.subject) ?? 0) + 1);
			sum.set(r.subject, (sum.get(r.subject) ?? 0) + r.value);
		}
		const top = leaders(p);
		if (top.length === 1) won.set(top[0].subject, (won.get(top[0].subject) ?? 0) + 1);
		const o = outcomeOf(p);
		if (o === 'varmq') wins.varmq++;
		else if (o === 'other') wins.other++;
		else wins.tied++;
	}
	const complete = [...count].filter(([, n]) => n === groups).map(([s]) => s);
	const partial = [...count].filter(([, n]) => n < groups).map(([subject, n]) => ({ subject, n }));
	const values = new Map(complete.map((s) => [s, sum.get(s)! / groups]));
	const base = complete.includes('VarMQ') ? 'VarMQ' : (complete.find(isVarmq) ?? null);
	const baseValue = base ? values.get(base)! : NaN;
	const lanes: Lane[] = complete.map((subject) => ({
		subject,
		value: values.get(subject)!,
		ratio: values.get(subject)! / baseValue,
		won: won.get(subject) ?? 0,
		of: groups,
	}));
	// Subjects that won a group but are not complete still count for the win totals above.
	const order = new Map([...count.keys()].map((s, i) => [s, i]));
	lanes.sort((a, b) =>
		state.sort === 'name'
			? a.subject.localeCompare(b.subject)
			: (state.sort === 'best' ? 1 : -1) * (m.better === 'high' ? b.value - a.value : a.value - b.value) || order.get(a.subject)! - order.get(b.subject)!,
	);
	return { lanes, base, groups, partial, wins };
}

export interface Loss {
	cell: Cell;
	winner: Ranked;
	varmq: Ranked;
	/** How many times better the winner is than the best VarMQ row, always at least 1. */
	ahead: number;
	rank: number;
	of: number;
}

export function lossesOf(cells: Cell[], m: Metric, state: State): Loss[] {
	const out: Loss[] = [];
	for (const cell of cells) {
		const p = ranked(cell, m, state);
		// A group VarMQ leads, or ties for the lead, is not a loss.
		if (!p.length || outcomeOf(p) !== 'other') continue;
		const i = p.findIndex((r) => isVarmq(r.subject));
		if (i < 0) continue;
		const winner = p[0];
		const varmq = p[i];
		const ahead = m.better === 'high' ? winner.value / varmq.value : varmq.value / winner.value;
		out.push({ cell, winner, varmq, ahead, rank: i + 1, of: p.length });
	}
	return out.sort((a, b) => b.ahead - a.ahead);
}

/** How often each non-VarMQ subject beats VarMQ in the scope. */
export function lossCounts(losses: Loss[]): { subject: string; n: number }[] {
	const n = new Map<string, number>();
	for (const l of losses) n.set(l.winner.subject, (n.get(l.winner.subject) ?? 0) + 1);
	return [...n].map(([subject, count]) => ({ subject, n: count })).sort((a, b) => b.n - a.n || a.subject.localeCompare(b.subject));
}

// ---- win map ----

export interface MapCell {
	cell: Cell;
	id: string;
	winner: Ranked | null;
	/** The rows that share the best value: the winner alone, or every row of a tie. */
	leaders: Ranked[];
	varmq: boolean;
	/** A VarMQ subject ties another subject for the best value. */
	tie: boolean;
}

export interface WinMap {
	/** Names what the row labels hold. */
	corner: string;
	/** One column group per source dataset in scope. */
	colGroups: { ds: Dataset; cols: { key: string; label: string }[] }[];
	rows: { key: string; label: string; cells: (MapCell | null)[][] }[];
}

function axes(ds: Dataset, g: Group): { row: string; rowLabel: string; col: string; colLabel: string } {
	const k = g.key;
	if (ds.suite === 'pool') {
		return { row: `${k.task}|${k.workers}`, rowLabel: `${k.task} · ${k.workers === '1' ? '1 worker' : k.workers}`, col: k.shape, colLabel: producerLabel(k.shape) };
	}
	if (ds.suite === 'redis') {
		if (k.test === 'enqueue') return { row: 'enqueue', rowLabel: 'Enqueue only', col: k.shape, colLabel: producerLabel(k.shape) };
		if (k.test === 'latency') return { row: 'latency', rowLabel: `Latency · ${k.workers} · C${k.instances}`, col: k.shape, colLabel: producerLabel(k.shape) };
		return { row: `${k.task}|${k.workers}|${k.instances}`, rowLabel: `${k.task === 'Sleep1ms' ? 'Sleep1ms' : k.task} · ${k.workers} · C${k.instances}`, col: k.shape, colLabel: producerLabel(k.shape) };
	}
	return { row: k.scenario, rowLabel: k.scenario, col: k.ckpt, colLabel: `${k.ckpt} pages` };
}

const CORNER: Record<Suite, string> = { pool: 'Task · workers', redis: 'Task · workers · instances', sqlite: 'Scenario' };

export function winMap(cells: Cell[], m: Metric, state: State): WinMap {
	const multi = new Set(cells.map((c) => c.ds.id)).size > 1;
	const colGroups: WinMap['colGroups'] = [];
	const rowKeys: { key: string; label: string }[] = [];
	for (const c of cells) {
		const a = axes(c.ds, c.g);
		let cg = colGroups.find((x) => x.ds === c.ds);
		if (!cg) colGroups.push((cg = { ds: c.ds, cols: [] }));
		if (!cg.cols.some((x) => x.key === a.col)) cg.cols.push({ key: a.col, label: a.colLabel });
		if (!rowKeys.some((x) => x.key === a.row)) rowKeys.push({ key: a.row, label: a.rowLabel });
	}
	const rows = rowKeys.map((r) => ({ ...r, cells: colGroups.map((cg) => cg.cols.map((): MapCell | null => null)) }));
	for (const c of cells) {
		const a = axes(c.ds, c.g);
		const gi = colGroups.findIndex((x) => x.ds === c.ds);
		const ci = colGroups[gi].cols.findIndex((x) => x.key === a.col);
		const ri = rowKeys.findIndex((x) => x.key === a.row);
		const p = ranked(c, m, state);
		const o = p.length ? outcomeOf(p) : null;
		rows[ri].cells[gi][ci] = { cell: c, id: cellId(c, multi), winner: p[0] ?? null, leaders: p.length ? leaders(p) : [], varmq: o === 'varmq', tie: o === 'tie' };
	}
	return { corner: CORNER[cells[0]?.ds.suite ?? 'pool'], colGroups, rows };
}

// ---- links and commands ----

export const BASE = 'https://github.com';

export function blobUrl(data: Data, ds: Dataset, line?: [number, number?]): string {
	const frag = line ? `#L${line[0]}${line[1] && line[1] !== line[0] ? `-L${line[1]}` : ''}` : '';
	return `${BASE}/${data.source.repo}/blob/${data.source.commit}/${ds.file}${frag}`;
}

export const groupUrl = (data: Data, c: Cell) => blobUrl(data, c.ds, [c.g.first, c.g.last]);
export const chartUrl = (view: View) => `https://benchmarks.goptics.org/${view.chart ? `?id=${view.chart}` : ''}`;
export const rawTreeUrl = (data: Data, path: string) => `${BASE}/${data.source.repo}/blob/${data.source.commit}/${path}`;

/** The `go test -bench` pattern that selects exactly the groups of the scope, as the Go tool splits it at "/". */
export function benchPattern(suite: Suite, f: Filters): string {
	const parts: string[] = [];
	if (suite === 'pool') {
		const { task, workers, shape } = f;
		const tw = task !== 'all' && workers !== 'all' ? `^${task}W${workers}$` : task !== 'all' ? `^${task}W` : workers !== 'all' ? `W${workers}$` : '';
		parts.push('Tasks', tw, shape !== 'all' ? `^${shape}$` : '');
	} else if (suite === 'redis') {
		if (f.test === 'enqueue') parts.push('Enqueue', '', f.shape !== 'all' ? `^${f.shape}$` : '');
		else if (f.test === 'latency') parts.push('Latency');
		else {
			const task = f.task !== 'all' ? f.task : '(Noop|Sleep1ms)';
			const workers = f.workers !== 'all' ? f.workers : '\\d+';
			parts.push('Durable', `^${task}W${workers}C${f.instances}$`, f.shape !== 'all' ? `^${f.shape}$` : '');
		}
	} else {
		parts.push('SQLite', f.scenario !== 'all' ? `^${f.scenario}$` : '', f.ckpt !== 'all' ? `^ckpt${f.ckpt}$` : '');
	}
	while (parts.length > 1 && parts[parts.length - 1] === '') parts.pop();
	return parts.join('/');
}

/** The filters that select exactly one group. */
export function groupFilters(g: Group): Filters {
	return { ...g.key };
}

/** The command that runs the scope, or only `only` when the lanes show one group. */
export function benchCommand(state: State, only?: Cell): string {
	const view = viewById(state.view);
	const f = only ? { ...state.f, ...groupFilters(only.g) } : state.f;
	const pattern = benchPattern(view.suite, f);
	const procs = only ? [only.ds.id] : view.sources;
	const cpus = view.suite === 'pool' ? procs.map((s) => s.replace(/^pool-(\d+)cpu$/, '$1')).join(',') : '';
	const run = `go test -run xxx -bench '${pattern}'${cpus ? ` -cpu ${cpus}` : ''} -benchtime=2x -benchmem -timeout 0`;
	return view.suite === 'redis' ? `task redis:up\ncd ${view.suite} && ${run}` : `cd ${view.suite} && ${run}`;
}

// ---- URL hash ----

export function toHash(data: Data, state: State): string {
	const d = defaultState(data, state.view);
	const p = new URLSearchParams();
	if (state.view !== VIEWS[0].id) p.set('view', state.view);
	for (const [k, v] of Object.entries(state.f)) if (v !== d.f[k]) p.set(k, v);
	if (state.metric !== 'rate') p.set('metric', state.metric);
	if (state.sort !== 'best') p.set('sort', state.sort);
	if (!state.baselines) p.set('baselines', '0');
	if (state.focus) p.set('focus', state.focus);
	return p.toString();
}

export function fromHash(data: Data, hash: string): State {
	const p = new URLSearchParams(hash.replace(/^#/, ''));
	const view = viewById(p.get('view') ?? VIEWS[0].id);
	const s = defaultState(data, view.id);
	const defs = filterDefs(data, view);
	for (const d of defs) {
		const v = p.get(d.id);
		if (v !== null) s.f[d.id] = v;
	}
	s.metric = p.get('metric') ?? s.metric;
	const sort = p.get('sort');
	if (sort === 'worst' || sort === 'name') s.sort = sort;
	if (p.get('baselines') === '0') s.baselines = false;
	s.focus = p.get('focus');
	return normalize(data, s);
}
