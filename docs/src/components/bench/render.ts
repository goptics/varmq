// HTML for the parts of the explorer that change with the filters.
// The build calls this for the default state of every view; the browser calls it again after each change.
// One renderer, so the page with and without JavaScript has the same markup and class names.

import {
	blobUrl,
	benchCommand,
	cellId,
	codeOf,
	currentMetric,
	fmt,
	fmtRatio,
	groupUrl,
	isVarmq,
	kindOf,
	label,
	laneSet,
	lossCounts,
	lossesOf,
	metricValue,
	noteOf,
	producerLabel,
	ranked,
	scopeOf,
	sourcesOf,
	viewById,
	winMap,
	workerLabel,
} from './lib.ts';
import type { Cell, Data, Loss, Metric, State } from './lib.ts';

export const esc = (s: string | number) =>
	String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);

const plural = (n: number, one: string, many = `${one}s`) => `${n.toLocaleString('en-US')} ${n === 1 ? one : many}`;

export function groupLabel(c: Cell, multi: boolean): string {
	const k = c.g.key;
	const cpus = multi ? `, ${c.ds.procs} CPUs` : '';
	if (c.ds.suite === 'pool') return `${k.task}, ${workerLabel(k.workers)}, ${producerLabel(k.shape)}${cpus}`;
	if (c.ds.suite === 'redis') {
		if (k.test === 'enqueue') return `Enqueue only, ${producerLabel(k.shape)}`;
		if (k.test === 'latency') return `Latency, ${k.workers} workers, C${k.instances}`;
		return `${k.task === 'Sleep1ms' ? 'Sleep 1 ms' : k.task}, ${k.workers} workers, C${k.instances}, ${producerLabel(k.shape)}`;
	}
	return `${k.scenario}, ${k.ckpt} pages`;
}

function sourceLink(data: Data, c: Cell, multi: boolean): string {
	const range = c.g.first === c.g.last ? `L${c.g.first}` : `L${c.g.first}–${c.g.last}`;
	const name = multi ? `${c.ds.id.replace('pool-', '')} ` : '';
	return `<a class="bsrc" href="${esc(groupUrl(data, c))}" aria-label="Lines ${c.g.first} to ${c.g.last} of ${esc(c.ds.file)} on GitHub">${esc(name)}${range}</a>`;
}

export interface Out {
	command: string;
	scope: string;
	lanes: string;
	laneNote: string;
	map: string;
	legend: string;
	losses: string;
	lossesSummary: string;
	lossesBody: string;
	rowsSummary: string;
	rowsBody: string;
}

/** The cells the lanes read: the whole scope, or the one focused group. */
export function laneCells(cells: Cell[], state: State): Cell[] {
	if (!state.focus) return cells;
	const multi = new Set(cells.map((c) => c.ds.id)).size > 1;
	const hit = cells.filter((c) => cellId(c, multi) === state.focus);
	return hit.length ? hit : cells;
}

export function renderAll(data: Data, state: State): Out {
	const view = viewById(state.view);
	const m = currentMetric(state);
	const cells = scopeOf(data, state);
	const multi = new Set(cells.map((c) => c.ds.id)).size > 1;
	const focused = laneCells(cells, state);
	const isFocus = !!state.focus && focused.length === 1 && focused !== cells;
	const total = sourcesOf(data, view).reduce((n, d) => n + d.groups.filter((g) => view.suite !== 'redis' || g.key.test === state.f.test).length, 0);

	return {
		command: renderCommand(state, isFocus ? focused[0] : undefined),
		scope: renderScope(data, state, cells, focused, isFocus, total, multi),
		...renderLanes(data, state, m, focused, isFocus),
		...renderMap(state, m, cells),
		...renderLosses(data, state, m, cells, multi),
		...renderRows(data, state, m, cells, multi),
	};
}

function renderCommand(state: State, only?: Cell): string {
	return benchCommand(state, only)
		.split('\n')
		.map((l) => `<span class="bcmd__line">${esc(l)}</span>`)
		.join('');
}

function renderScope(data: Data, state: State, cells: Cell[], focused: Cell[], isFocus: boolean, total: number, multi: boolean): string {
	if (!cells.length) {
		return `<p class="bscope bscope--empty">No group matches these filters. The matrix does not have every combination: <code>Loop10K</code> runs with one worker only. Clear a filter to continue.</p>`;
	}
	if (isFocus) {
		return `<p class="bscope"><span>One group: <strong>${esc(groupLabel(focused[0], multi))}</strong>. Each lane is one row of the raw file.</span> <button type="button" class="pbtn" data-unfocus hidden data-js>Back to ${plural(cells.length, 'group')}</button></p>`;
	}
	const subjects = new Set(cells.flatMap((c) => c.g.rows.map((r) => r[0]))).size;
	const of = cells.length < total ? ` of ${total.toLocaleString('en-US')}` : '';
	const what = cells.length === 1 ? 'Each lane is one row of the raw file.' : 'Each lane is the mean over these groups.';
	return `<p class="bscope"><span><strong>${plural(cells.length, 'group')}</strong>${of} in scope · ${plural(subjects, 'subject')}. ${what}</span></p>`;
}

// ---- lanes ----

function renderLanes(data: Data, state: State, m: Metric, cells: Cell[], isFocus: boolean): Pick<Out, 'lanes' | 'laneNote'> {
	const set = laneSet(cells, m, state);
	if (!set.lanes.length) return { lanes: '', laneNote: '' };
	const max = Math.max(...set.lanes.map((l) => l.value));
	const baseLane = set.lanes.find((l) => l.subject === set.base);
	const ref = baseLane ? (baseLane.value / max).toFixed(4) : '0';
	const better = m.better === 'high' ? 'higher is better' : 'lower is better';
	const unitHead = m.id === 'rate' ? 'Rate' : m.id === 'memory' ? 'Allocated' : m.id === 'wal' ? 'Peak WAL' : 'Latency';
	const scale = cells.length > 1 ? ` · mean of ${cells.length.toLocaleString('en-US')} groups` : '';
	const line = new Map<string, number>();
	if (isFocus) for (const r of ranked(cells[0], m, state)) line.set(r.subject, r.line);

	const items = set.lanes
		.map((l, i) => {
			const ahead = l.subject !== set.base && (m.better === 'high' ? l.ratio > 1 : l.ratio < 1);
			const sub = isFocus
				? `<a class="bsrc" href="${esc(blobUrl(data, cells[0].ds, [line.get(l.subject) ?? cells[0].g.first]))}" aria-label="Line ${line.get(l.subject)} of ${esc(cells[0].ds.file)} on GitHub">L${line.get(l.subject)}</a>`
				: `won ${l.won.toLocaleString('en-US')} of ${l.of.toLocaleString('en-US')}`;
			const x = l.subject === set.base ? '1.00×' : fmtRatio(l.ratio);
			const word = l.subject === set.base ? 'the baseline' : ahead ? 'ahead of ' + set.base : 'behind ' + set.base;
			return `<li class="rlane${isVarmq(l.subject) ? ' is-varmq' : ''}${ahead ? ' is-ahead' : ''}${kindOf(l.subject) === 'baseline' ? ' is-baseline' : ''}" style="--s:${(l.value / max).toFixed(4)};--i:${i}">
<span class="rlane__name"><span class="rlane__subject" title="${esc(noteOf(l.subject))}">${esc(label(l.subject))}</span><span class="rlane__sub">${sub}</span></span>
<span class="rlane__track" aria-hidden="true"><span class="rlane__bar"></span></span>
<span class="rlane__val">${fmt(l.value, m)}<span class="rlane__unit"> ${esc(m.unit === 'MB' || m.unit === 'ms' ? m.unit : '')}</span></span>
<span class="rlane__x"><span aria-hidden="true">${x}</span><span class="sr-only">${esc(x.replace('×', ' times'))} ${esc(set.base ?? 'VarMQ')}, ${word}</span></span>
</li>`;
		})
		.join('');

	const heading = `<div class="rg__title"><h4 id="${esc(state.view)}-lanes">${esc(m.title)}</h4><span>${esc(m.unit)} · ${better}${scale}</span></div>`;
	const head = `<div class="rlane__head" aria-hidden="true"><span>Subject</span><span></span><span>${unitHead}</span><span>× ${esc(set.base ?? 'VarMQ')}</span></div>`;
	const list = `<ol class="rlanes" style="--ref:${ref}" aria-labelledby="${esc(state.view)}-lanes">${items}</ol>`;

	const notes: string[] = [];
	notes.push(
		`The dashed line marks ${esc(set.base ?? 'VarMQ')}. An amber ratio means the lane is ahead of it. The “×” column divides the lane by ${esc(set.base ?? 'VarMQ')}${set.base === 'VarMQDistributed' ? ' (VarMQ does not run with four instances)' : ''}.`,
	);
	if (set.lanes.some((l) => kindOf(l.subject) === 'baseline'))
		notes.push('† A baseline, not a library: N goroutines reading one channel. The buffered one holds all 1M tasks.');
	if (set.partial.length)
		notes.push(
			`Not in the lanes: ${set.partial.map((p) => `${esc(label(p.subject))} (${p.n} of ${set.groups} groups)`).join(', ')}. A mean over unequal groups would not compare like with like. Narrow the filters to see ${set.partial.length === 1 ? 'it' : 'them'}.`,
		);
	return { lanes: heading + head + list, laneNote: notes.map((n) => `<p>${n}</p>`).join('') };
}

// ---- win map ----

function leadVerb(m: Metric): string {
	if (m.id === 'rate') return 'is fastest, at';
	if (m.id === 'memory') return 'uses the least memory, at';
	if (m.id === 'wal') return 'has the smallest WAL, at';
	return `has the lowest ${m.label.toLowerCase()} latency, at`;
}

function renderMap(state: State, m: Metric, cells: Cell[]): Pick<Out, 'map' | 'legend'> {
	if (!cells.length) return { map: '', legend: '' };
	const map = winMap(cells, m, state);
	const multi = map.colGroups.length > 1;
	const wins = new Map<string, number>();
	let first = true;

	const head1 = multi
		? `<tr><th scope="col" class="bmap__corner"></th>${map.colGroups.map((cg) => `<th scope="colgroup" colspan="${cg.cols.length}" class="bmap__cpu">${cg.ds.procs} CPUs</th>`).join('')}</tr>`
		: '';
	const short = (l: string) => l.replace(/ producers?$/, '').replace(/ pages$/, '');
	const head2 = `<tr><th scope="col" class="bmap__corner">${esc(map.corner)}</th>${map.colGroups
		.map((cg) => cg.cols.map((c) => `<th scope="col" class="bmap__col"><span aria-hidden="true">${esc(short(c.label))}</span><span class="sr-only">${esc(c.label)}</span></th>`).join(''))
		.join('')}</tr>`;

	const body = map.rows
		.map((row) => {
			const tds = row.cells
				.map((cols) =>
					cols
						.map((mc) => {
							if (!mc || !mc.winner) return `<td class="bmap__empty"></td>`;
							const w = mc.winner.subject;
							wins.set(w, (wins.get(w) ?? 0) + 1);
							const p = ranked(mc.cell, m, state);
							const vi = p.findIndex((r) => isVarmq(r.subject));
							const where = `${groupLabel(mc.cell, multi)}`;
							const text = `${where}: ${w} ${leadVerb(m)} ${fmt(mc.winner.value, m)} ${m.unit}.${vi > 0 ? ` VarMQ is ${vi + 1} of ${p.length}.` : ''}`;
							const tab = first ? 0 : -1;
							first = false;
							return `<td><button type="button" class="bcell ${mc.varmq ? 'is-varmq' : 'is-other'}" data-cell="${esc(mc.id)}" data-text="${esc(text)}" tabindex="${tab}" aria-pressed="false" aria-label="${esc(text)}">${esc(codeOf(w))}</button></td>`;
						})
						.join(''),
				)
				.join('');
			return `<tr><th scope="row" class="bmap__row">${esc(row.label)}</th>${tds}</tr>`;
		})
		.join('');

	const table = `<div class="bmap__scroll"><table class="bmap__t"><thead>${head1}${head2}</thead><tbody>${body}</tbody></table></div>
<p class="bmap__read" data-read aria-hidden="true">Hover or focus a square to read it. Press it to show that group in the lanes.</p>`;

	const order = [...wins.keys()].sort((a, b) => Number(isVarmq(b)) - Number(isVarmq(a)) || wins.get(b)! - wins.get(a)! || a.localeCompare(b));
	const legend = `<ul class="blegend" aria-label="Fastest subject, with the number of groups it leads">${order
		.map((s) => `<li class="${isVarmq(s) ? 'is-varmq' : 'is-other'}"><span class="blegend__code">${esc(codeOf(s))}</span><span>${esc(label(s))}</span><strong>${wins.get(s)!.toLocaleString('en-US')}</strong></li>`)
		.join('')}</ul>`;
	return { map: table, legend };
}

// ---- where VarMQ does not win ----

function renderLosses(data: Data, state: State, m: Metric, cells: Cell[], multi: boolean): Pick<Out, 'losses' | 'lossesSummary' | 'lossesBody'> {
	if (!cells.length) return { losses: '', lossesSummary: '', lossesBody: '' };
	const set = laneSet(cells, m, state);
	const losses = lossesOf(cells, m, state);
	const groups = set.groups;
	const best = m.better === 'high' ? 'fastest' : 'lowest';
	const noun = m.id === 'rate' ? 'throughput' : m.id === 'memory' ? 'memory use' : m.id === 'wal' ? 'WAL size' : `${m.label.toLowerCase()} latency`;

	if (!losses.length) {
		return {
			losses: `<p class="bloss__lead">VarMQ has the ${best} ${noun} in ${set.wins.varmq === groups ? 'every one of the' : ''} <strong>${plural(groups, 'group')}</strong> in this scope.</p>`,
			lossesSummary: '',
			lossesBody: '',
		};
	}
	const counts = lossCounts(losses);
	const lead = `<p class="bloss__lead">Another subject has the ${best} ${noun} in <strong>${plural(losses.length, 'group')}</strong> of ${groups.toLocaleString('en-US')}. VarMQ has it in <strong>${set.wins.varmq.toLocaleString('en-US')}</strong>.</p>
<ul class="bloss__counts">${counts.map((c) => `<li><strong>${esc(label(c.subject))}</strong> <span>${plural(c.n, 'group')}</span></li>`).join('')}</ul>`;

	const rows = losses
		.map((l) => lossRow(data, l, m, multi))
		.join('');
	const table = `<div class="btable"><table class="btable__t btable__t--loss"><thead><tr><th scope="col">Group</th><th scope="col">Best</th><th scope="col">VarMQ</th><th scope="col">Ahead by</th><th scope="col">Source</th></tr></thead><tbody>${rows}</tbody></table></div>`;
	return { losses: lead, lossesSummary: `All ${plural(losses.length, 'group')} where VarMQ does not win, biggest gap first`, lossesBody: table };
}

function lossRow(data: Data, l: Loss, m: Metric, multi: boolean): string {
	const id = cellId(l.cell, multi);
	return `<tr>
<th scope="row" data-l="Group"><span>${esc(groupLabel(l.cell, multi))}</span> <button type="button" class="blink" data-focus="${esc(id)}" hidden data-js>Show</button></th>
<td data-l="Best"><strong>${esc(label(l.winner.subject))}</strong> <span class="n">${fmt(l.winner.value, m)}</span></td>
<td data-l="VarMQ">${esc(l.varmq.subject === 'VarMQ' ? '' : l.varmq.subject + ' ')}<span class="n">${fmt(l.varmq.value, m)}</span> <span class="rank">${l.rank} of ${l.of}</span></td>
<td data-l="Ahead by" class="n bamber">${fmtRatio(l.ahead)}</td>
<td data-l="Source">${sourceLink(data, l.cell, multi)}</td>
</tr>`;
}

// ---- every row ----

function renderRows(data: Data, state: State, m: Metric, cells: Cell[], multi: boolean): Pick<Out, 'rowsSummary' | 'rowsBody'> {
	if (!cells.length) return { rowsSummary: 'Rows', rowsBody: '' };
	const subjects: string[] = [];
	for (const c of cells) for (const r of c.g.rows) if (!subjects.includes(r[0]) && (state.baselines || kindOf(r[0]) !== 'baseline')) subjects.push(r[0]);
	const head = `<tr><th scope="col">Group</th>${subjects.map((s) => `<th scope="col" class="${isVarmq(s) ? 'is-varmq' : ''}" title="${esc(noteOf(s))}">${esc(label(s))}</th>`).join('')}<th scope="col">Source</th></tr>`;
	const body = cells
		.map((c) => {
			const p = ranked(c, m, state);
			const vals = new Map(c.g.rows.map((r) => [r[0], metricValue(c.ds, c.g, r, m)]));
			const tds = subjects
				.map((s) => {
					const v = vals.get(s);
					if (v === undefined) return `<td class="n bmiss"><span aria-hidden="true">–</span><span class="sr-only">not run</span></td>`;
					const best = p[0]?.subject === s;
					return `<td class="n${best ? ' is-best' : ''}">${fmt(v, m)}${best ? '<span class="sr-only"> (best)</span>' : ''}</td>`;
				})
				.join('');
			return `<tr><th scope="row">${esc(groupLabel(c, multi))}</th>${tds}<td>${sourceLink(data, c, multi)}</td></tr>`;
		})
		.join('');
	return {
		rowsSummary: `Every row behind these numbers · ${plural(cells.length, 'group')}`,
		rowsBody: `<div class="btable"><table class="btable__t"><caption class="sr-only">${esc(m.title)} (${esc(m.unit)}) for each group in scope. The best value of each group is marked.</caption><thead>${head}</thead><tbody>${body}</tbody></table></div>`,
	};
}

