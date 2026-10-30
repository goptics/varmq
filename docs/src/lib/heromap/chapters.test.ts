import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { CHAPTERS, STATIC_AT, TOTAL, plain, starts } from './chapters.ts';
import { endState, poolStates } from './pool.ts';
import { PIECES, RINGS } from './scene.ts';
import { queueStates, ringStates, trayStates, workers } from './stores.ts';

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
		assert.deepEqual(CHAPTERS[9].captions.map(c => plain(c.text)), ['One worker model, from memory to SQLite']);
		assert.equal(plain(CHAPTERS[9].captions[0].swap.text), 'Redis');
		assert.deepEqual(CHAPTERS[4].captions.map(c => plain(c.text).split(' ')[0]), ['Priority,', 'MaxLen', 'MinLen', 'RoundRobin']);
		assert.equal(CHAPTERS.flatMap(c => c.captions).length, 25);
	});
	// The site plays a step per scroll rather than the loop, so the loop may run long enough to show every strategy.
	it('run about 100 seconds', () => {
		assert.ok(TOTAL >= 80000 && TOTAL <= 105000, String(TOTAL));
		assert.equal(starts()[9] + CHAPTERS[9].duration, TOTAL);
	});
	// The site stops on each chapter's last frame, so its last job's done (a 900 ms pulse and check) must have settled.
	it('finish every part a second before its chapter ends', () => {
		for (const c of CHAPTERS) for (const p of c.parts)
			assert.ok(Math.max(...p.events.map(e => e.at)) <= c.duration - 1000, `chapter ${c.n}`);
	});
	it('chapter 1 never has more than one job waiting at a dispatch', () => {
		for (const d of dispatches(part(1, 'pool', 'core'))) assert.ok(d.waiting <= 0, String(d.at));
	});
	it('chapter 2 opens with TunePool(5), grows to 5 goroutines, and ends with one idle', () => {
		const p = part(2, 'pool', 'core');
		assert.deepEqual(p.plan.controls[0], { at: 1300, op: 'tune', to: 5 });
		assert.equal(Math.max(...poolStates(p.plan, p.events).map(s => s.busy.length)), 5);
		assert.equal(endState(p.plan, p.events).idle.length, 1);
	});
	it('the core worker starts each chapter where the last one left it', () => {
		let last;
		for (const c of CHAPTERS) for (const p of c.parts) if (p.kind === 'pool' && p.worker === 'core') {
			if (last) assert.deepEqual({ limit: p.plan.limit, idle: p.plan.idle, turn: p.plan.turn }, last, `chapter ${c.n}`);
			last = endState(p.plan, p.events);
		}
		// Round robin goes on from the source after the last one served, as varmq keeps its index for the worker's life.
		// It starts at the queue in chapter 4, where a second source first binds; chapter 9 takes only from the queue,
		// so chapter 10 looks at the priority queue first and finds the topic's copy before the queue's jobs.
		assert.equal(part(4, 'pool', 'core').plan.turn, 0);
		assert.equal(part(10, 'pool', 'core').plan.turn, 1);
		assert.deepEqual(dispatches(part(10, 'pool', 'core')).map(d => d.job), ['zt0-core', 'zj0', 'zj1']);
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
	it('chapter 9 starts nothing while paused or stopped, empties the pool at the stop, and runs what waited after Restart', () => {
		const p = part(9, 'pool', 'core');
		const at = op => p.plan.controls.find(c => c.op === op).at;
		const [pause, resume, stop, restart] = ['pause', 'resume', 'stop', 'restart'].map(at);
		assert.ok(dispatches(p).every(d => d.at < pause || (d.at >= resume && d.at < stop) || d.at >= restart));
		// The stop lets the running jobs finish, then every goroutine goes before the restart.
		const states = poolStates(p.plan, p.events);
		assert.ok(states.some(s => s.at > stop && s.at < restart && s.busy.length === 0 && s.idle.length === 0));
		// The job due at the stop and the two added while stopped run after the restart.
		assert.deepEqual(dispatches(p).filter(d => d.at >= restart).map(d => d.job), ['k4', 'k5', 'k6']);
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
	it('show one caption at a time, each risen in full before it leaves, gone before the chapter ends', () => {
		// Words rise 55 ms apart, each over 520 ms, and a caption starts to leave at `until`, which takes 300 ms and 30 ms
		// more per word. The story page lists the captions beside the map and hides the map's own, so a caption may be
		// short, as long as what it is about, and the next may come before it has left.
		for (const c of CHAPTERS) c.captions.forEach((cap, i) => {
			const words = plain(cap.text).split(' ').length;
			assert.ok(cap.at >= 0 && cap.at + 55 * (words - 1) + 520 <= cap.until, `chapter ${c.n}: ${cap.text}`);
			assert.ok(cap.until + 300 + 30 * words <= c.duration, `chapter ${c.n}: ${cap.text}`);
			if (i > 0) assert.ok(c.captions[i - 1].until <= cap.at, `chapter ${c.n}: ${cap.text}`);
			for (const mark of ['*', '!', '#']) assert.equal(cap.text.split(mark).length % 2, 1, `unpaired ${mark} in ${cap.text}`);
			// A swap waits for every word to have risen, and its roll (420 ms) is over before the line leaves.
			if (cap.swap) {
				assert.ok(cap.at + 55 * (words - 1) + 520 <= cap.swap.at, `chapter ${c.n}: ${cap.text}`);
				assert.ok(cap.swap.at + 420 <= cap.until, `chapter ${c.n}: ${cap.text}`);
			}
		});
	});
	it('show the finale\'s first caption, fully risen, in the still frame', () => {
		// Words rise 55 ms apart, each over 520 ms (the choreographer's caption rule).
		const cap = CHAPTERS[9].captions[0], words = plain(cap.text).split(' ').length;
		assert.ok(starts()[9] + cap.at + 55 * (words - 1) + 520 <= STATIC_AT, cap.text);
		assert.ok(STATIC_AT < starts()[9] + cap.until);
		// The still frame shows the caption's own words, so it comes before the swap.
		assert.ok(STATIC_AT < starts()[9] + cap.swap.at);
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
	it('hand a worker at limit 1 one job at a time, never a batch', () => {
		// varmq hands a lone worker every job waiting at once, which the map has no way to draw. Chapter 1's core, chapter
		// 3's result worker, chapter 6's second worker and chapter 10's two lone workers run at limit 1.
		let lone = 0;
		for (const c of CHAPTERS) for (const p of c.parts) {
			if (p.kind !== 'pool') continue;
			let limit = p.plan.limit;
			for (const e of p.events) {
				if (e.type === 'control' && e.op === 'tune') limit = e.to;
				if (e.type !== 'dispatch' || limit !== 1) continue;
				assert.equal(e.waiting, 0, `chapter ${c.n}: ${p.worker} at ${e.at}`);
				lone++;
			}
		}
		assert.ok(lone >= 5, String(lone));
	});
	it('draw only on workers, lanes and rings the map has by then', () => {
		for (const c of CHAPTERS) for (const p of c.parts) {
			if (p.kind === 'pool') for (const s of p.plan.sources)
				assert.ok(piece[s.id]?.kind === 'lane' && piece[s.id].chapter <= c.n, `chapter ${c.n}: ${s.id}`);
			for (const e of p.events) if (['dispatch', 'take', 'spawn', 'exit'].includes(e.type)) {
				const id = card(p, e);
				assert.ok(piece[id].chapter <= c.n && e.ring < RINGS[id].length, `chapter ${c.n}: ${id} ring ${e.ring}`);
			}
			// A worker is never given more goroutines than the rings of its card, and it starts on rings that are drawn.
			if (p.kind === 'pool') {
				const limits = [p.plan.limit, ...(p.plan.controls ?? []).filter(k => k.op === 'tune').map(k => k.to)];
				assert.ok(Math.max(...limits) <= RINGS[p.worker].length, `chapter ${c.n}: ${p.worker}`);
			} else for (const w of workers(p.plan)) {
				const places = RINGS[card(p, { consumer: w.id })].length;
				assert.ok(w.limit <= places && w.idle.every(i => i < places), `chapter ${c.n}: ${w.id}`);
			}
		}
		// The TunePool(5) chip takes the place of the Restart() control that chapter 9 switches on.
		assert.deepEqual(CHAPTERS[1].chips, [{ id: 'ctl-restart', label: 'TunePool(5)', at: 1300, until: 2500 }]);
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
	it('chapter 5 runs one burst per strategy, each leaving in that strategy\'s order', () => {
		// a is alerts (the queue), r reports (the priority queue) and e emails (the third queue), bound in that order.
		const letter = { queue: 'a', prio: 'r', 'queue-c': 'e' };
		const runs = {};
		for (const d of dispatches(part(5, 'pool', 'core'))) runs[d.strategy] = (runs[d.strategy] ?? '') + letter[d.source];
		assert.deepEqual(runs, {
			priority: 'aarrre', // in bind order, though r is the longest and e the shortest
			maxlen: 'rrare', // r while it is the longest, then the first of equals
			minlen: 'reeaaa', // the shortest with a job first
			roundrobin: 'areare', // each in turn, from the turn chapter 4 left
		});
	});
	it('chapter 7 tells the store fixture\'s story, and keeps the program down for 2.6 s', () => {
		const ev = part(7, 'sqlite').events, index = type => ev.findIndex(e => e.type === type);
		// s0 and s2 run when the program dies, and the restart puts them back ahead of s3
		assert.deepEqual(trayStates(ev)[index('crash')].tray.filter(Boolean), ['s0', 's2']);
		assert.deepEqual(queueStates(ev)[index('requeue')].order, ['s0', 's2', 's3']);
		// The program stays down, its box red and its card empty, long enough to read the caption about it.
		assert.ok(ev[index('restart')].at - ev[index('crash')].at >= 2600);
	});
	it('chapter 8 has B die mid-job, and Redis put its jobs back at the front and wake C for them', () => {
		const ev = part(8, 'redis').events, index = type => ev.findIndex(e => e.type === type);
		assert.deepEqual(ev[index('crash')], { at: 4000, type: 'crash', consumer: 'b' });
		// B's two jobs and C's two are in flight when B dies, and C finishes its own before B's come back.
		assert.deepEqual(trayStates(ev)[index('crash')].tray.filter(Boolean), ['r0', 'r1', 'r2', 'r3']);
		assert.ok(ev.filter(e => e.type === 'ack' && ['r2', 'r3'].includes(e.job)).every(e => e.at < ev[index('requeue')].at));
		assert.deepEqual(queueStates(ev)[index('requeue')].order, ['r0', 'r1']);
		// One wake, to C, which takes both at once.
		const after = ev.slice(index('requeue'));
		assert.deepEqual(after.filter(e => e.type === 'notify').map(e => e.consumer), ['c']);
		assert.deepEqual(after.filter(e => e.type === 'take').map(e => [e.at, e.job, e.consumer]), [[6920, 'r0', 'c'], [6920, 'r1', 'c']]);
	});
	it('run each store worker as a pool, ending every chapter with one idle goroutine', () => {
		for (const c of CHAPTERS) for (const p of c.parts) {
			if (p.kind === 'pool') continue;
			for (const [id, r] of Object.entries(ringStates(p.plan, p.events).at(-1).rings))
				assert.deepEqual([r.busy.length, r.idle.length], [0, 1], `chapter ${c.n}: ${id}`);
		}
	});
	it('start and stop store goroutines when varmq would', () => {
		const changes = (n, kind) => part(n, kind).events.filter(e => e.type === 'spawn' || e.type === 'exit')
			.map(e => `${e.at} ${e.type} ${e.consumer} ${e.ring}`);
		assert.deepEqual(changes(7, 'sqlite'), [
			'3300 spawn sqlite 1', // the first claim takes two jobs, and one goroutine is idle
			'6800 exit sqlite 0', '6800 exit sqlite 1', // the program dies with both
			'9400 spawn sqlite 0', // and restarts with one
			'10900 spawn sqlite 1', // the claim after the requeue takes two again
			'15900 exit sqlite 1', // at s3's ack nothing waits, and ring 0 is idle
		]);
		assert.deepEqual(changes(8, 'redis'), [
			'2420 spawn b 1', '2520 spawn c 1', // each woken worker pulls two jobs, the second on a new goroutine
			'4000 exit b 0', '4000 exit b 1', // program B dies with both
			'5370 exit c 0', // C's last ack, with nothing waiting and its other goroutine idle
			'6920 spawn c 0', // woken for B's jobs, C pulls both
			'9000 spawn b 0', // B restarts with one
			'9970 exit c 1',
		]);
		// The finale's store workers start with one ring each, the one their last chapter left idle.
		assert.deepEqual(workers(part(10, 'sqlite').plan).map(w => w.idle), [[0]]);
		assert.deepEqual(workers(part(10, 'redis').plan).map(w => w.idle), [[0], [0]]);
	});
	it('hold no more records in flight than busy rings, but for a dead program\'s, until the requeue', () => {
		for (const c of CHAPTERS) for (const p of c.parts) {
			if (p.kind === 'pool') continue;
			const rings = ringStates(p.plan, p.events), tray = trayStates(p.events);
			let down = false; // from the crash to the requeue, the records of the jobs it ran stay in flight, with no ring
			p.events.forEach((e, i) => {
				if (e.type === 'crash') down = true;
				if (e.type === 'requeue') down = false;
				const busy = Object.values(rings[i].rings).reduce((n, r) => n + r.busy.length, 0);
				if (!down) assert.ok(tray[i].tray.filter(Boolean).length <= busy, `chapter ${c.n} at ${e.at}`);
			});
		}
	});
	it('chapter 9 lands jobs while the worker is paused', () => {
		const p = part(9, 'pool', 'core'), at = op => p.plan.controls.find(k => k.op === op).at;
		assert.ok(p.events.some(e => e.type === 'land' && e.at > at('pause') && e.at < at('resume')));
	});
});
