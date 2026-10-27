import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { CHAPTERS, STATIC_AT, TOTAL, plain, starts } from './chapters.ts';
import { endState, poolStates } from './pool.ts';
import { PIECES, RINGS } from './scene.ts';
import { queueStates, trayStates } from './stores.ts';

const part = (n, kind, worker?) => CHAPTERS[n - 1].parts.find(p => p.kind === kind && (!worker || p.worker === worker));
const dispatches = p => p.events.filter(e => e.type === 'dispatch');

describe('chapters', () => {
	it('are the agreed ten, in order', () => {
		assert.deepEqual(CHAPTERS.map(c => c.title), ['One worker', 'The pool', 'Results and errors', 'Priority',
			'Many queues, one worker', 'Topics', 'SQLite', 'Redis', 'Control', 'Everything together']);
	});
	it('carry the agreed captions', () => {
		assert.equal(plain(CHAPTERS[0].captions[0].text), 'Add() puts jobs in a queue');
		assert.equal(plain(CHAPTERS[6].captions[3].text), 'The program dies mid-job');
		assert.equal(plain(CHAPTERS[9].captions[1].text), 'Stop() lets running jobs finish');
		assert.equal(CHAPTERS.flatMap(c => c.captions).length, 20);
	});
	it('run about 85 seconds', () => {
		assert.ok(TOTAL >= 80000 && TOTAL <= 95000, String(TOTAL));
		assert.equal(starts()[9] + CHAPTERS[9].duration, TOTAL);
	});
	it('finish every part before its chapter ends', () => {
		for (const c of CHAPTERS) for (const p of c.parts)
			assert.ok(Math.max(...p.events.map(e => e.at)) <= c.duration - 600, `chapter ${c.n}`);
	});
	it('chapter 1 never has more than one job waiting at a dispatch', () => {
		for (const d of dispatches(part(1, 'pool', 'core'))) assert.ok(d.waiting <= 0, String(d.at));
	});
	it('chapter 2 opens with TunePool(3), stays under 2 × 3 waiting, and ends with one idle goroutine', () => {
		const p = part(2, 'pool', 'core');
		assert.deepEqual(p.plan.controls[0], { at: 1300, op: 'tune', to: 3 });
		for (const d of dispatches(p)) assert.ok(d.waiting + 1 < 6, String(d.at));
		assert.equal(endState(p.plan, p.events).idle.length, 1);
	});
	it('the core worker starts each chapter where the last one left it', () => {
		let last;
		for (const c of CHAPTERS) for (const p of c.parts) if (p.kind === 'pool' && p.worker === 'core') {
			if (last) assert.deepEqual({ limit: p.plan.limit, idle: p.plan.idle }, last, `chapter ${c.n}`);
			last = endState(p.plan, p.events);
		}
	});
	it('chapter 4 runs the lowest number first', () => {
		assert.deepEqual(dispatches(part(4, 'pool', 'core')).map(e => e.job), ['p1', 'p2', 'p3', 'p5', 'p8']);
	});
	it('chapter 7 puts the crashed jobs back at the head and acks all four', () => {
		const ev = part(7, 'sqlite').events;
		const rq = ev.find(e => e.type === 'requeue');
		assert.deepEqual(queueStates(ev).find(s => s.at === rq.at).order.slice(0, rq.jobs.length), rq.jobs);
		assert.equal(ev.filter(e => e.type === 'ack').length, 4);
	});
	it('chapter 8 never holds more than 4 in flight', () => {
		for (const s of trayStates(part(8, 'redis').events)) assert.ok(s.tray.filter(Boolean).length <= 4);
	});
	it('chapter 9 starts nothing while paused and reaches 5 busy after TunePool(5)', () => {
		const p = part(9, 'pool', 'core');
		const pause = p.plan.controls.find(c => c.op === 'pause').at, resume = p.plan.controls.find(c => c.op === 'resume').at;
		assert.ok(dispatches(p).every(d => d.at < pause || d.at >= resume));
		assert.equal(Math.max(...poolStates(p.plan, p.events).map(s => s.busy.length)), 5);
	});
	it('chapter 10 ends with Stop and no goroutine left on the core worker', () => {
		const p = part(10, 'pool', 'core');
		assert.ok(p.plan.controls.some(c => c.op === 'stop'));
		assert.deepEqual(endState(p.plan, p.events).idle, []);
		assert.ok(CHAPTERS[9].parts.every(q => Math.min(...q.events.map(e => e.at)) >= 1500));
		assert.equal(STATIC_AT, starts()[9] + 1300);
	});
});

// What the tests above leave open: what the renderer and the choreographer rely on, and the rest of each story.
describe('chapter rules', () => {
	const piece = Object.fromEntries(PIECES.map(p => [p.id, p]));
	// The card a job runs in, which is also the key of its rings: a pool's worker, the SQLite worker, or Redis's b or c.
	const card = (p, e) => p.kind === 'pool' ? p.worker : p.kind === 'sqlite' ? 'sqlite' : `redis-${e.consumer}`;

	it('give every job and published value its own id across the loop', () => {
		// The renderer makes one element per id, and a store job's record shares its job's id.
		const ids = CHAPTERS.flatMap(c => [...c.parts.flatMap(p => p.plan.jobs.map(j => j.id)), ...(c.published ?? []).map(v => v.id)]);
		assert.equal(new Set(ids).size, ids.length);
	});
	it('show one caption at a time, each for at least 1.4 s and 500 ms after the last, gone before the chapter ends', () => {
		// A caption starts to leave at `until`, and leaving takes 300 ms and 30 ms more per word.
		for (const c of CHAPTERS) c.captions.forEach((cap, i) => {
			const words = plain(cap.text).split(' ').length;
			assert.ok(cap.at >= 0 && cap.until - cap.at >= 1400, `chapter ${c.n}: ${cap.text}`);
			assert.ok(cap.until + 300 + 30 * words <= c.duration, `chapter ${c.n}: ${cap.text}`);
			if (i > 0) assert.ok(c.captions[i - 1].until + 500 <= cap.at, `chapter ${c.n}: ${cap.text}`);
			for (const mark of ['*', '!']) assert.equal(cap.text.split(mark).length % 2, 1, `unpaired ${mark} in ${cap.text}`);
		});
	});
	it('show the finale\'s first caption, fully risen, in the still frame', () => {
		// Words rise 55 ms apart, each over 520 ms (the choreographer's caption rule).
		const cap = CHAPTERS[9].captions[0], words = plain(cap.text).split(' ').length;
		assert.ok(starts()[9] + cap.at + 55 * (words - 1) + 520 <= STATIC_AT, cap.text);
		assert.ok(STATIC_AT < starts()[9] + cap.until);
	});
	it('start nothing before the camera has arrived', () => {
		for (const c of CHAPTERS) for (const p of c.parts) assert.ok(p.events[0].at >= 1300, `chapter ${c.n} ${p.kind}`);
	});
	it('finish every job they add: done on a worker, acked in a store', () => {
		for (const c of CHAPTERS) for (const p of c.parts) {
			const end = p.kind === 'pool' ? 'done' : 'ack';
			for (const j of p.plan.jobs) assert.ok(p.events.some(e => e.type === end && e.job === j.id), `chapter ${c.n}: ${j.id}`);
		}
	});
	it('keep every lane within its five slots, and hand each goroutine one job at a time', () => {
		for (const c of CHAPTERS) for (const p of c.parts) {
			if (p.kind !== 'pool') {
				for (const s of queueStates(p.events)) assert.ok(s.order.length <= 5, `chapter ${c.n} ${p.kind} at ${s.at}`);
				continue;
			}
			for (const s of poolStates(p.plan, p.events)) for (const [id, queue] of Object.entries(s.waiting))
				assert.ok(queue.length <= 5, `chapter ${c.n}: ${id} at ${s.at}`);
			// Above a limit of 1, fewer than 2 × limit jobs wait at every hand-off, so each goroutine is handed one.
			let limit = p.plan.limit;
			for (const e of p.events) {
				if (e.type === 'control' && e.op === 'tune') limit = e.to;
				if (e.type === 'dispatch' && limit > 1) assert.ok(e.waiting + 1 < 2 * limit, `chapter ${c.n} at ${e.at}`);
			}
		}
	});
	it('draw only on workers, lanes and rings the map has by then', () => {
		for (const c of CHAPTERS) for (const p of c.parts) {
			if (p.kind === 'pool') for (const s of p.plan.sources)
				assert.ok(piece[s.id]?.kind === 'lane' && piece[s.id].chapter <= c.n, `chapter ${c.n}: ${s.id}`);
			for (const e of p.events) if (e.type === 'dispatch' || e.type === 'take') {
				const id = card(p, e);
				assert.ok(piece[id].chapter <= c.n && e.ring < RINGS[id].length, `chapter ${c.n}: ${id} ring ${e.ring}`);
			}
			// A pool is never tuned past the rings of its card, and a store's worker has exactly the rings drawn for it.
			if (p.kind === 'pool') {
				const limits = [p.plan.limit, ...(p.plan.controls ?? []).filter(k => k.op === 'tune').map(k => k.to)];
				assert.ok(Math.max(...limits) <= RINGS[p.worker].length, `chapter ${c.n}: ${p.worker}`);
			} else if (p.kind === 'sqlite') assert.equal(p.plan.rings, RINGS.sqlite.length, `chapter ${c.n}`);
			else for (const w of p.plan.consumers) assert.equal(w.rings, RINGS[`redis-${w.id}`].length, `chapter ${c.n}: ${w.id}`);
		}
		// The TunePool(3) chip takes the place of the tune control that chapter 9 switches on.
		assert.deepEqual(CHAPTERS[1].chips, [{ id: 'ctl-tune', label: 'TunePool(3)', at: 1300, until: 2500 }]);
		assert.ok(CHAPTERS.flatMap(c => c.chips ?? []).every(chip => piece[chip.id]?.kind === 'chip'));
	});
	it('give the core and the second worker a copy of every published value, as it is published', () => {
		// A copy is named after its value and its worker: t0-core, t0-second.
		for (const c of CHAPTERS) {
			const copies = c.parts.flatMap(p => p.kind === 'pool' ? p.plan.jobs.filter(j => j.source === 'topic').map(j => [j.id, j.at]) : []);
			const values = (c.published ?? []).flatMap(v => ['core', 'second'].map(w => [`${v.id}-${w}`, v.at]));
			assert.deepEqual(copies.sort(), values.sort(), `chapter ${c.n}`);
		}
		assert.equal(CHAPTERS[5].published.length, 2);
	});
	it('chapter 1 shows a job waiting while another runs', () => {
		const p = part(1, 'pool', 'core');
		assert.ok(poolStates(p.plan, p.events).some(s => s.busy.length > 0 && s.waiting.queue.length > 0));
	});
	it('chapter 3 hands back a value, then an error', () => {
		const p = part(3, 'pool', 'result');
		assert.deepEqual(dispatches(p).map(d => p.outcomes[d.job]), ['value', 'error']);
	});
	it('chapter 5 takes from its queues in turn and skips the empty priority queue', () => {
		const p = part(5, 'pool', 'core');
		assert.deepEqual(dispatches(p).map(d => d.source), ['queue', 'queue-c', 'queue', 'queue-c']);
		assert.ok(p.plan.sources.some(s => s.id === 'prio') && !p.plan.jobs.some(j => j.source === 'prio'));
	});
	it('chapter 7 tells the store fixture\'s story, and shows the store alone before the restart', () => {
		const ev = part(7, 'sqlite').events, index = type => ev.findIndex(e => e.type === type);
		// s0 and s2 run when the program dies, and the restart puts them back ahead of s3
		assert.deepEqual(trayStates(ev)[index('crash')].tray.filter(Boolean), ['s0', 's2']);
		assert.deepEqual(queueStates(ev)[index('requeue')].order, ['s0', 's2', 's3']);
		// The program stage has faded by the crash + 1350, and only the store and its two records hold the frame
		// until the restart, for about 1.2 s.
		assert.ok(ev[index('restart')].at - ev[index('crash')].at >= 2600);
	});
	it('chapter 9 lands jobs while the worker is paused', () => {
		const p = part(9, 'pool', 'core'), at = op => p.plan.controls.find(k => k.op === op).at;
		assert.ok(p.events.some(e => e.type === 'land' && e.at > at('pause') && e.at < at('resume')));
	});
});
