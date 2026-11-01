#!/usr/bin/env node
// Builds src/components/bench/data.json from the raw `go test -bench` output in github.com/goptics/benchmarks.
//
//   node scripts/bench-data.mjs                      rebuild from the commit already in data.json
//   node scripts/bench-data.mjs --ref main           resolve a branch or SHA on GitHub, then rebuild
//   node scripts/bench-data.mjs --dir ../../bench    read a local checkout instead of GitHub
//   node scripts/bench-data.mjs --check              rebuild in memory and fail if data.json differs
//   node scripts/bench-data.mjs --check-live         also compare every row with benchmarks.goptics.org
//
// Every number in data.json is copied from one line of a .txt file. The line number stays in the row,
// so the page links each figure to github.com/goptics/benchmarks/blob/<commit>/<file>#L<line>.
// The docs build reads data.json only and never needs the network.

import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const OUT = resolve(here, '../src/components/bench/data.json');
const REPO = 'goptics/benchmarks';

const SUITES = {
	pool: {
		readme: 'pool/README.md',
		files: [
			{ id: 'pool-24cpu', file: 'pool/bench/pool-24cpu.txt', procs: 24, title: 'Worker pool, 24 CPUs' },
			{ id: 'pool-8cpu', file: 'pool/bench/pool-8cpu.txt', procs: 8, title: 'Worker pool, 8 CPUs' },
			{ id: 'pool-4cpu', file: 'pool/bench/pool-4cpu.txt', procs: 4, title: 'Worker pool, 4 CPUs' },
		],
	},
	redis: {
		readme: 'redis/README.md',
		files: [{ id: 'redis', file: 'redis/bench/redis.txt', procs: 22, title: 'Redis queues' }],
	},
	sqlite: {
		readme: 'sqlite/README.md',
		files: [{ id: 'sqlite', file: 'sqlite/bench/sqlite.txt', procs: 24, title: 'SQLite queues' }],
	},
};

// ---- args ----

const args = process.argv.slice(2);
const flag = (name) => args.includes(`--${name}`);
const opt = (name) => {
	const i = args.indexOf(`--${name}`);
	return i >= 0 ? args[i + 1] : undefined;
};

// ---- sources ----

async function fetchText(url) {
	const res = await fetch(url, { headers: { 'user-agent': 'varmq-docs-bench-data' } });
	if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`);
	return res.text();
}

async function resolveSource() {
	const dir = opt('dir');
	if (dir) {
		const root = resolve(dir);
		const git = (...a) => execFileSync('git', ['-C', root, ...a], { encoding: 'utf8' }).trim();
		const commit = git('rev-parse', 'HEAD');
		if (git('status', '--porcelain', '--', '.')) throw new Error(`${root} has local changes; commit or stash them first`);
		return {
			commit,
			commitDate: git('show', '-s', '--format=%cI', commit),
			read: async (path) => readFileSync(join(root, path), 'utf8'),
		};
	}
	let ref = opt('ref');
	if (!ref && existsSync(OUT)) ref = JSON.parse(readFileSync(OUT, 'utf8')).source.commit;
	if (!ref) throw new Error('No data.json yet. Pass --ref <branch|sha> or --dir <checkout>.');
	const info = JSON.parse(await fetchText(`https://api.github.com/repos/${REPO}/commits/${ref}`));
	const commit = info.sha;
	return {
		commit,
		commitDate: info.commit.committer.date,
		read: (path) => fetchText(`https://raw.githubusercontent.com/${REPO}/${commit}/${path}`),
	};
}

// ---- go test -bench parser ----

/** One `go test -bench` file: header lines, one entry per Benchmark line, the closing `ok` line. */
export function parseBenchFile(text) {
	const header = {};
	const rows = [];
	let seconds = null;
	text.split('\n').forEach((raw, i) => {
		const line = i + 1;
		const h = /^(goos|goarch|pkg|cpu): (.*)$/.exec(raw);
		if (h) return void (header[h[1]] = h[2].trim());
		const ok = /^ok\s+\S+\s+([\d.]+)s$/.exec(raw);
		if (ok) return void (seconds = Number(ok[1]));
		if (!raw.startsWith('Benchmark')) return;
		const f = raw.trim().split(/\s+/);
		const name = f[0];
		const m = /^(.*)-(\d+)$/.exec(name);
		if (!m) throw new Error(`line ${line}: no -<procs> suffix in ${name}`);
		if ((f.length - 2) % 2 !== 0) throw new Error(`line ${line}: odd number of value and unit fields`);
		const metrics = [];
		for (let k = 2; k < f.length; k += 2) {
			const v = Number(f[k]);
			if (!Number.isFinite(v)) throw new Error(`line ${line}: ${f[k]} is not a number`);
			metrics.push([f[k + 1], v]);
		}
		rows.push({ line, path: m[1].replace(/^Benchmark/, ''), procs: Number(m[2]), iterations: Number(f[1]), metrics });
	});
	return { header, rows, seconds };
}

// A benchmark path is <Func>/<case>/<shape>/<subject>. The first three parts name a group of rows
// that run the same work with different subjects. The Go -bench pattern for the group is those three parts.
function groupKey(suite, parts) {
	const [fn, name, shape] = parts;
	if (suite === 'pool') {
		const m = /^(.*)W(\d+K?)$/.exec(name);
		if (fn !== 'Tasks' || !m) throw new Error(`unexpected pool name ${parts.join('/')}`);
		return { task: m[1], workers: m[2], shape };
	}
	if (suite === 'redis') {
		if (fn === 'Enqueue') return { test: 'enqueue', shape };
		const m = /^(Noop|Sleep1ms|Latency)W(\d+)C(\d+)$/.exec(name);
		if (!m) throw new Error(`unexpected redis name ${parts.join('/')}`);
		if (fn === 'Latency') return { test: 'latency', workers: m[2], instances: m[3], shape };
		if (fn !== 'Durable') throw new Error(`unexpected redis function ${fn}`);
		return { test: 'durable', task: m[1], workers: m[2], instances: m[3], shape };
	}
	if (suite === 'sqlite') {
		const m = /^ckpt(\d+)$/.exec(shape);
		if (fn !== 'SQLite' || !m) throw new Error(`unexpected sqlite name ${parts.join('/')}`);
		return { scenario: name, ckpt: m[1] };
	}
	throw new Error(`unknown suite ${suite}`);
}

function buildDataset(suite, spec, text) {
	const { header, rows, seconds } = parseBenchFile(text);
	const colsets = [];
	const groups = [];
	const index = new Map();
	for (const r of rows) {
		if (r.procs !== spec.procs) throw new Error(`${spec.file}:${r.line}: -${r.procs} suffix, expected -${spec.procs}`);
		const parts = r.path.split('/');
		const subject = parts[3];
		const id = parts.slice(0, 3).join('/');
		const cols = r.metrics.map(([u]) => u);
		let ci = colsets.findIndex((c) => c.join() === cols.join());
		if (ci < 0) ci = colsets.push(cols) - 1;
		let g = index.get(id);
		if (!g) {
			g = { id, key: groupKey(suite, parts), c: ci, first: r.line, last: r.line, rows: [] };
			index.set(id, g);
			groups.push(g);
		}
		if (g.c !== ci) throw new Error(`${spec.file}:${r.line}: columns differ inside group ${id}`);
		if (g.rows.some((x) => x[0] === subject)) throw new Error(`${spec.file}:${r.line}: ${subject} twice in ${id}`);
		if (r.line !== g.last + (g.rows.length ? 1 : 0)) throw new Error(`${spec.file}:${r.line}: group ${id} is not contiguous`);
		g.last = r.line;
		g.rows.push([subject, r.line, r.iterations, ...r.metrics.map(([, v]) => v)]);
	}
	return {
		id: spec.id,
		suite,
		title: spec.title,
		file: spec.file,
		procs: spec.procs,
		cpu: header.cpu,
		goos: header.goos,
		goarch: header.goarch,
		pkg: header.pkg,
		seconds,
		rowCount: rows.length,
		colsets,
		groups,
	};
}

// ---- README parser ----

const stripLinks = (s) => s.replace(/\[([^\]]+)\]\([^)]*\)/g, '$1');

function parseEnvironment(readme) {
	const m = /## Environment\n([\s\S]*?)\n## /.exec(readme);
	if (!m) throw new Error('README.md has no Environment section');
	const body = m[1].trim();
	const [machine, pinning] = body.split(/\n\n/).map((p) => stripLinks(p.trim()));
	const versions = [];
	for (const line of body.split('\n')) {
		if (!line.startsWith('|') || /^\|[-\s|]+\|$/.test(line)) continue;
		const cells = line.split('|').slice(1, -1).map((c) => c.trim());
		if (cells[0] === 'Library') continue;
		const link = /\[([^\]]+)\]\(([^)]+)\)/.exec(cells[0]);
		versions.push({
			name: link ? link[1] : cells[0],
			label: stripLinks(cells[0]),
			url: link ? link[2] : null,
			version: stripLinks(cells[1]),
			suites: cells[2].split(',').map((s) => s.trim()),
		});
	}
	if (!machine || !pinning || !versions.length) throw new Error('README.md Environment section did not parse');
	return { machine, pinning, versions };
}

function runDate(readme, path) {
	const m = /Run of (\d{4}-\d{2}-\d{2})/.exec(readme);
	if (!m) throw new Error(`${path} has no "Run of <date>" line`);
	return m[1];
}

// ---- build ----

async function build() {
	const src = await resolveSource();
	const rootReadme = await src.read('README.md');
	const env = parseEnvironment(rootReadme);
	const datasets = [];
	const runs = new Set();
	for (const [suite, def] of Object.entries(SUITES)) {
		const readme = await src.read(def.readme);
		const date = runDate(readme, def.readme);
		runs.add(date);
		const claimed = [...readme.matchAll(/(\d+) rows/g)].map((x) => Number(x[1]));
		for (const spec of def.files) {
			const ds = buildDataset(suite, spec, await src.read(spec.file));
			ds.runDate = date;
			ds.readme = def.readme;
			// The suite README states how many rows each file has. A different count means a partial or edited file.
			if (!claimed.includes(ds.rowCount)) throw new Error(`${spec.file}: ${ds.rowCount} rows, README says ${claimed.join(' or ')}`);
			datasets.push(ds);
		}
	}
	if (runs.size !== 1) throw new Error(`suites report different run dates: ${[...runs].join(', ')}`);
	return {
		schema: 1,
		source: { repo: REPO, commit: src.commit, commitDate: src.commitDate, runDate: [...runs][0] },
		env,
		datasets,
	};
}

// One group per line keeps a diff of data.json readable.
function serialize(data) {
	const ds = data.datasets.map((d) => {
		const { groups, ...rest } = d;
		const head = JSON.stringify(rest).slice(0, -1);
		return `${head},"groups":[\n${groups.map((g) => JSON.stringify(g)).join(',\n')}\n]}`;
	});
	return `{"schema":${data.schema},\n"source":${JSON.stringify(data.source)},\n"env":${JSON.stringify(data.env)},\n"datasets":[\n${ds.join(',\n')}\n]}\n`;
}

// ---- live check ----

async function checkLive(data) {
	const catalog = JSON.parse(await fetchText('https://benchmarks.goptics.org/dataset'));
	let matched = 0;
	const problems = [];
	const unchartedRows = [];
	for (const d of data.datasets) {
		const live = JSON.parse(await fetchText(`https://benchmarks.goptics.org/dataset/${d.id}`));
		const byKey = new Map();
		for (const g of d.groups) {
			const [, name, shape] = g.id.split('/');
			for (const r of g.rows) byKey.set(`${name}|${shape}|${r[0]}`, { g, r });
		}
		const seen = new Set();
		for (const x of live.data) {
			const k = `${x.name}|${x.yAxis}|${x.xAxis}`;
			const hit = byKey.get(k);
			if (!hit) {
				problems.push(`${d.id}: chart row ${k} is not in the raw file`);
				continue;
			}
			seen.add(k);
			const cols = d.colsets[hit.g.c];
			const val = (unit) => (cols.includes(unit) ? hit.r[3 + cols.indexOf(unit)] : undefined);
			for (const s of x.stats) {
				let raw;
				let got = s.value;
				if (s.type.startsWith('Throughput')) raw = val('ops/s') ?? val('jobs/s');
				else if (s.type.startsWith('Memory Usage')) {
					raw = Math.round((val('B/op') / 1048576) * 100) / 100; // vizb prints MiB under the label MB/op
				} else if (s.type.startsWith('Allocations')) raw = val('allocs/op');
				else if (s.type.startsWith('Execution Time')) {
					raw = Math.round((val('ns/op') / 1e9) * 100) / 100;
				} else if (/^Metric \(.+\)$/.test(s.type)) raw = val(/^Metric \((.+)\)$/.exec(s.type)[1]);
				else {
					problems.push(`${d.id}: ${k}: unknown stat ${s.type}`);
					continue;
				}
				// The chart rounds memory, time and the Metric columns to 2 decimals; every other column is exact.
				const rounded = s.type.startsWith('Memory') || s.type.startsWith('Execution') || s.type.startsWith('Metric');
				const tol = rounded ? 0.0051 : 0;
				if (raw === undefined || Math.abs(raw - got) > tol) problems.push(`${d.id}: ${k}: ${s.type} chart ${got}, raw ${raw}`);
			}
			matched++;
		}
		for (const [k, v] of byKey) if (!seen.has(k)) unchartedRows.push(`${d.id}: ${k} (line ${v.r[1]})`);
	}
	console.log(`live check: ${matched} chart rows match the raw files; catalog lists ${catalog.map((c) => c.id).join(', ')}`);
	console.log(`raw rows the charts leave out: ${unchartedRows.length}`);
	for (const u of unchartedRows.slice(0, 20)) console.log(`  ${u}`);
	if (problems.length) {
		console.error(`${problems.length} problems:\n${problems.slice(0, 40).join('\n')}`);
		process.exitCode = 1;
	}
}

// ---- main ----

if (process.argv[1] === fileURLToPath(import.meta.url)) {
	const data = await build();
	const text = serialize(data);
	if (flag('check')) {
		const same = existsSync(OUT) && readFileSync(OUT, 'utf8') === text;
		console.log(same ? 'data.json matches the raw files.' : 'data.json differs from the raw files.');
		if (!same) process.exitCode = 1;
	} else {
		writeFileSync(OUT, text);
		const rows = data.datasets.reduce((n, d) => n + d.rowCount, 0);
		console.log(`wrote ${OUT}: ${data.datasets.length} datasets, ${rows} rows, commit ${data.source.commit.slice(0, 10)}, run ${data.source.runDate}`);
	}
	if (flag('check-live')) await checkLive(data);
}
