import { Check, Minus } from 'lucide-react';
import { Fragment } from 'react';
import { CopyCommand } from '@/components/home/copy-command';
import { TubeStory, type StoryStep } from '@/components/home/tube-story';
import { CHAPTERS, TOTAL, plain, starts } from '@/lib/heromap/chapters.ts';
import { LOGOS } from '@/lib/heromap/render.ts';
import { codeSteps } from '@/lib/home/compile.ts';
import { marks } from '@/lib/home/marks.tsx';
import {
	BENCHMARKS,
	CHARTS,
	COMPARISON,
	FEATURE_NOTES,
	FEATURES,
	POOL_RUNS,
	POOLS,
	REDIS_RUNS,
	RUN,
	SQLITE_RUNS,
	type Mark,
} from '@/lib/home/proof.ts';
import { STATIONS } from '@/lib/home/stations.ts';
import { gitConfig, installCommand, quickStartUrl } from '@/lib/shared';

const GITHUB = `https://github.com/${gitConfig.user}/${gitConfig.repo}`;
const PKG = 'https://pkg.go.dev/github.com/goptics/varmq';
// Step 1 also plays the zoom from the whole map into chapter 1, which closes the loop; the finale stops on the whole
// map, before that zoom.
const ZOOM_IN = 1200;
const FINALE = 7000;

const n = (x: number) => x.toLocaleString('en-US');

function Logo({ id }: { id: 's-store' | 'r-store' }) {
	return (
		<svg className={`hp-logo hp-logo-${LOGOS[id].name}`} viewBox="0 0 24 24" aria-hidden="true">
			<path d={LOGOS[id].d} />
		</svg>
	);
}

const has = (mark: Mark) => (typeof mark === 'string' ? mark === 'yes' : mark.yes);
const noteOf = (mark: Mark) => (typeof mark === 'string' ? undefined : mark.note);

function Cell({ mark }: { mark: Mark }) {
	const yes = has(mark);
	const note = noteOf(mark);
	return (
		<td data-yes={yes}>
			<span className="hp-mark">
				{yes ? <Check aria-label="yes" /> : <Minus aria-label="no" />}
				{note && <sup>{note}</sup>}
			</span>
		</td>
	);
}

const captionText = (c: (typeof CHAPTERS)[number]['captions'][number]) => (c.swap ? `${c.text} to ${c.swap.text}` : c.text);

/** Each chapter as a step of the story: its stretch of the loop, its captions' times, and the code lines they light. */
function story(): StoryStep[] {
	const begin = starts();
	return CHAPTERS.map((chapter, i) => {
		const station = STATIONS[i];
		const lead = i === 0 ? ZOOM_IN : 0;
		return {
			from: i === 0 ? TOTAL - ZOOM_IN : begin[i],
			ms: lead + (i === CHAPTERS.length - 1 ? FINALE : chapter.duration),
			captions: chapter.captions.map((c) => lead + c.at),
			lines: { wide: station.lines, phone: station.phone?.lines ?? station.lines },
			title: chapter.title,
			notes: chapter.captions.map((c) => marks(captionText(c))),
			source: (
				<>
					Full program: <a href={station.source.href}>{station.source.label}</a>
				</>
			),
		};
	});
}

/** The story as text: each step's title, captions and code, in order. */
function storyText() {
	return (
		<ol>
			{CHAPTERS.map((chapter, i) => (
				<li key={i}>
					<h2>{chapter.title}</h2>
					<p>{chapter.captions.map((c) => plain(captionText(c))).join('. ')}.</p>
					<pre>
						<code>{STATIONS[i].code}</code>
					</pre>
				</li>
			))}
		</ol>
	);
}

export default async function HomePage() {
	const hero = (
		<header className="hp-hero">
			<h1 className="hp-hero-logo">
				<img src="/logo-dark.svg" alt="VarMQ" width={432} height={112} />
			</h1>
			<p className="hp-hero-pitch">
				A typed worker pool and message queue for Go. One worker model, from memory to SQLite to Redis.
			</p>
			<CopyCommand command={installCommand} />
			<p className="hp-hero-links">
				<a className="hp-go" href={quickStartUrl}>
					Read the quick start
				</a>
				<a href={GITHUB}>GitHub</a>
			</p>
			<p className="hp-hero-hint">Scroll, and each scroll runs the next step on the map.</p>
		</header>
	);

	return (
		<main className="hp-page">
			<TubeStory hero={hero} steps={story()} code={await codeSteps()} text={storyText()} />

			<section className="hp-section" aria-labelledby="hp-compare">
				<h2 id="hp-compare" className="hp-section-title">
					How it compares
				</h2>
				<p className="hp-section-lead">
					Against the other Go worker pools, checked against each package&apos;s source in October 2026.
				</p>
				<div className="hp-table-wrap hp-features-wrap">
					<table className="hp-table hp-features">
						<thead>
							<tr>
								<th scope="col">Feature</th>
								{POOLS.map((p) => (
									<th key={p} scope="col">
										{p}
									</th>
								))}
							</tr>
						</thead>
						<tbody>
							{FEATURES.map((row) => (
								<tr key={row.feature}>
									<th scope="row">{row.feature}</th>
									{row.marks.map((mark, i) => (
										<Cell key={i} mark={mark} />
									))}
								</tr>
							))}
						</tbody>
					</table>
				</div>
				{/* Narrow screens: each feature with the other pools that have it, instead of eight columns. */}
				<ul className="hp-features-list">
					{FEATURES.map((row) => {
						const others = POOLS.slice(1).flatMap((pool, i) =>
							has(row.marks[i + 1]) ? [{ pool, note: noteOf(row.marks[i + 1]) }] : [],
						);
						const noted = POOLS.slice(1).flatMap((pool, i) =>
							!has(row.marks[i + 1]) && noteOf(row.marks[i + 1]) ? [{ pool, note: noteOf(row.marks[i + 1]) }] : [],
						);
						return (
							<li key={row.feature}>
								<span className="hp-feature">{row.feature}</span>
								<span className="hp-also">
									{others.length === 0
										? 'Only VarMQ'
										: others.map((o, i) => (
												<Fragment key={o.pool}>
													{i === 0 ? 'Also ' : ', '}
													{o.pool}
													{o.note && <sup>{o.note}</sup>}
												</Fragment>
											))}
									{noted.map((o) => (
										<Fragment key={o.pool}>
											; not {o.pool}
											<sup>{o.note}</sup>
										</Fragment>
									))}
								</span>
							</li>
						);
					})}
				</ul>
				<ol className="hp-notes">
					{FEATURE_NOTES.map((note) => (
						<li key={note}>{note}</li>
					))}
				</ol>

				<h3 className="hp-sub-title">15 million tasks on 50,000 workers</h3>
				<p className="hp-section-lead">
					Seconds to run them all and memory per million tasks, lower is better; throughput, higher is better.{' '}
					{RUN}
				</p>
				<div className="hp-table-wrap">
					<table className="hp-table hp-runs">
						<thead>
							<tr>
								<th scope="col">Pool</th>
								<th scope="col">4 CPUs (s)</th>
								<th scope="col">24 CPUs (s)</th>
								<th scope="col">Million tasks/s</th>
								<th scope="col">Memory (MB)</th>
							</tr>
						</thead>
						<tbody>
							{POOL_RUNS.map((r) => (
								<tr key={r.pool} data-us={r.pool === 'VarMQ' || undefined}>
									<th scope="row">
										{r.pool}
										{r.handRolled && <sup>*</sup>}
									</th>
									<td>{r.cpu4.toFixed(2)}</td>
									<td>{r.cpu24.toFixed(2)}</td>
									<td>{r.rate.toFixed(2)}</td>
									<td>{r.mb}</td>
								</tr>
							))}
						</tbody>
					</table>
				</div>
				<p className="hp-notes-text">
					<sup>*</sup> Hand-rolled pools: fixed workers reading a channel. VarMQ uses more memory because{' '}
					<code>Add</code> never blocks: it keeps every task as a job until a worker takes it, where ants and conc
					make the producer wait. Raw results and the harness are in{' '}
					<a href={BENCHMARKS}>goptics/benchmarks</a>, the charts at <a href={CHARTS}>benchmarks.goptics.org</a>,
					and the full comparison in the <a href={COMPARISON}>README</a>.
				</p>
			</section>

			<section className="hp-section" aria-labelledby="hp-storage">
				<h2 id="hp-storage" className="hp-section-title">
					The storage under the queue
				</h2>
				<p className="hp-section-lead">
					The worker stays the same; its queue lives where the adapter puts it. Both adapters are goptics packages.
				</p>
				<div className="hp-plates">
					<article className="hp-plate">
						<h3>
							<Logo id="s-store" />
							SQLite
						</h3>
						<p>
							<a href="https://github.com/goptics/sqliteq">sqliteq</a> keeps each queue in a SQLite file, so its jobs
							survive a restart.
						</p>
						<div className="hp-table-wrap">
						<table className="hp-table hp-mini">
							<thead>
								<tr>
									<th scope="col">Jobs/s</th>
									<th scope="col">Mixed</th>
									<th scope="col">Drain</th>
								</tr>
							</thead>
							<tbody>
								{SQLITE_RUNS.map((r) => (
									<tr key={r.library} data-us={r.library === 'VarMQ' || undefined}>
										<th scope="row">{r.library}</th>
										<td>{n(r.mixed)}</td>
										<td>{n(r.drain)}</td>
									</tr>
								))}
							</tbody>
						</table>
						</div>
					</article>
					<article className="hp-plate">
						<h3>
							<Logo id="r-store" />
							Redis
						</h3>
						<p>
							<a href="https://github.com/goptics/redisq">redisq</a> shares one queue between programs and puts a
							dead consumer&apos;s jobs back.
						</p>
						<div className="hp-table-wrap">
						<table className="hp-table hp-mini">
							<thead>
								<tr>
									<th scope="col">Jobs/s, 100 workers</th>
									<th scope="col">1 instance</th>
									<th scope="col">4 instances</th>
								</tr>
							</thead>
							<tbody>
								{REDIS_RUNS.map((r) => (
									<tr key={r.library} data-us={r.library === 'VarMQ' || undefined}>
										<th scope="row">{r.library}</th>
										<td>{n(r.c1)}</td>
										<td>{n(r.c4)}</td>
									</tr>
								))}
							</tbody>
						</table>
						</div>
						<p className="hp-plate-note">
							At 10 workers gocraft/work is faster, and with a single producer rmq is.
						</p>
					</article>
					<article className="hp-plate">
						<h3>Your own</h3>
						<p>
							Implement{' '}
							<a href={`${PKG}#IPersistentQueue`}>
								<code>IPersistentQueue</code>
							</a>{' '}
							or{' '}
							<a href={`${PKG}#IDistributedQueue`}>
								<code>IDistributedQueue</code>
							</a>{' '}
							for any storage, and bind it with <code>WithPersistentQueue</code> or <code>WithDistributedQueue</code>,
							as the adapters are.
						</p>
					</article>
				</div>
				<p className="hp-notes-text">
					{RUN} SQLite: 20,000 jobs per run, on WAL. Redis: 100,000 jobs from 100 producers, Redis 7.2 on its own
					core. Every row is in <a href={BENCHMARKS}>goptics/benchmarks</a>.
				</p>
			</section>

			<section className="hp-section hp-close" aria-labelledby="hp-install">
				<h2 id="hp-install" className="hp-section-title">
					Start with one worker
				</h2>
				<CopyCommand command={installCommand} />
				<p className="hp-hero-links">
					<a className="hp-go" href={quickStartUrl}>
						Read the quick start
					</a>
					<a href={PKG}>API reference</a>
					<a href={GITHUB}>GitHub</a>
				</p>
			</section>

			<footer className="hp-footer">
				<p>
					VarMQ is MIT licensed. <a href={GITHUB}>goptics/varmq</a> · <a href={PKG}>pkg.go.dev</a> ·{' '}
					<a href="https://varmq.goptics.org/bench-log/">Bench log</a>
				</p>
			</footer>
		</main>
	);
}
