import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { endState, poolStates, simulatePool, type PoolPlan } from './pool.ts';

const pace = { wake: 400, gap: 450, after: 300 };
const burst = (n: number, work = 2000, source = 'q') =>
	Array.from({ length: n }, (_, i) => ({ id: `j${i}`, source, at: 100 + 100 * i, work }));
const plan = (p: Partial<PoolPlan>): PoolPlan => ({ ...pace, limit: 3, idle: [0], sources: [{ id: 'q' }], jobs: [], ...p });
const dispatches = ev => ev.filter(e => e.type === 'dispatch');

describe('simulatePool', () => {
	it('reuses the idle goroutine, then starts new ones up to the limit', () => {
		const ev = simulatePool(plan({ jobs: burst(5) }));
		assert.deepEqual(dispatches(ev).slice(0, 3).map(e => [e.job, e.ring, e.spawned]),
			[['j0', 0, false], ['j1', 1, true], ['j2', 2, true]]);
		const p = plan({ jobs: burst(5) });
		assert.equal(Math.max(...poolStates(p, simulatePool(p)).map(s => s.busy.length)), 3);
	});
	it('ends with exactly one idle goroutine', () => {
		const p = plan({ jobs: burst(5) });
		assert.equal(endState(p, simulatePool(p)).idle.length, 1);
	});
	it('hands out the lowest priority number first', () => {
		const jobs = [5, 2, 8, 1, 3].map((pr, i) => ({ id: `p${pr}`, source: 'pq', at: 100 + 50 * i, work: 2500, priority: pr }));
		const ev = simulatePool(plan({ sources: [{ id: 'pq', priority: true }], jobs }));
		assert.deepEqual(dispatches(ev).map(e => e.job), ['p1', 'p2', 'p3', 'p5', 'p8']);
	});
	it('round robin takes from each queue in turn and skips empty ones', () => {
		const jobs = [{ id: 'a0', source: 'a' }, { id: 'a1', source: 'a' }, { id: 'c0', source: 'c' }, { id: 'c1', source: 'c' }]
			.map((j, i) => ({ ...j, at: 100 + 10 * i, work: 3000 }));
		const ev = simulatePool(plan({ limit: 1, sources: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], strategy: 'roundrobin', jobs: [] }));
		assert.equal(ev.length, 0);
		const rr = simulatePool(plan({ limit: 4, sources: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], strategy: 'roundrobin', jobs }));
		assert.deepEqual(dispatches(rr).map(e => e.job), ['a0', 'c0', 'a1', 'c1']);
	});
	it('starts nothing while paused and lets running jobs finish', () => {
		const p = plan({ jobs: [...burst(2), { id: 'late', source: 'q', at: 3000, work: 500 }], controls: [{ at: 2500, op: 'pause' }, { at: 6000, op: 'resume' }] });
		const ev = simulatePool(p);
		assert.ok(dispatches(ev).filter(e => e.at > 2500 && e.at < 6000).length === 0);
		assert.ok(ev.some(e => e.type === 'done' && e.at > 2500 && e.at < 6000));
		assert.ok(dispatches(ev).some(e => e.job === 'late' && e.at >= 6000));
	});
	it('tune raises the limit', () => {
		const p = plan({ controls: [{ at: 50, op: 'tune', to: 5 }], jobs: burst(5, 4000) });
		assert.equal(Math.max(...poolStates(p, simulatePool(p)).map(s => s.busy.length)), 5);
	});
	it('stop lets running jobs finish and every goroutine exits', () => {
		const p = plan({ jobs: burst(2, 3000), controls: [{ at: 1500, op: 'stop' }] });
		const ev = simulatePool(p);
		assert.equal(ev.filter(e => e.type === 'done').length, 2);
		assert.deepEqual(endState(p, ev).idle, []);
	});
	it('refuses a batch at limit 1', () => {
		assert.throws(() => simulatePool(plan({ limit: 1, jobs: burst(3) })), /batch/);
	});
});

// What the tests above leave open: when things happen, which goroutine gets what, and what the replays say.
describe('pool rules', () => {
	it('wakes after a land, spaces a run by gap, and wakes again after a done', () => {
		// j0 lands at 100, so the loop wakes at 500. The wake-ups of the later lands come due mid-run and are dropped;
		// the run hands out at 500, 950 and 1400 and stops at the limit. Ring 0 is done at 2500 and wakes the loop 300 on.
		const ev = simulatePool(plan({ jobs: burst(5) }));
		assert.deepEqual(dispatches(ev).map(e => [e.at, e.job, e.ring, e.spawned, e.waiting]), [
			[500, 'j0', 0, false, 4], [950, 'j1', 1, true, 3], [1400, 'j2', 2, true, 2],
			[2800, 'j3', 0, false, 1], [3250, 'j4', 1, false, 0],
		]);
	});
	it('finishes a job work after its dispatch and keeps a goroutine only while it is needed', () => {
		// Rings 0 and 1 stay while a job waits, ring 2 because none is idle. With the queue empty and ring 2 idle, the rest go.
		const ev = simulatePool(plan({ jobs: burst(5) }));
		assert.deepEqual(ev.filter(e => e.type === 'done').map(e => [e.at, e.job]),
			[[2500, 'j0'], [2950, 'j1'], [3400, 'j2'], [4800, 'j3'], [5250, 'j4']]);
		assert.deepEqual(ev.filter(e => e.type === 'idle' || e.type === 'exit').map(e => [e.at, e.type, e.ring]),
			[[2500, 'idle', 0], [2950, 'idle', 1], [3400, 'idle', 2], [4800, 'exit', 0], [5250, 'exit', 1]]);
	});
	it('keeps a goroutine idle for each job that waits', () => {
		// Paused with two jobs waiting: the first two goroutines to finish stay for them, the third has none to stay for.
		const jobs = [...burst(3), { id: 'w0', source: 'q', at: 1500, work: 500 }, { id: 'w1', source: 'q', at: 1600, work: 500 }];
		const p = plan({ jobs, controls: [{ at: 2000, op: 'pause' }] });
		assert.deepEqual(endState(p, simulatePool(p)).idle, [0, 1]);
	});
	it('hands a job to the goroutine that went idle last, and starts new ones on the lowest free ring', () => {
		const ev = simulatePool(plan({ limit: 4, idle: [0, 2], jobs: burst(4) }));
		assert.deepEqual(dispatches(ev).map(e => [e.job, e.ring, e.spawned]),
			[['j0', 2, false], ['j1', 0, false], ['j2', 1, true], ['j3', 3, true]]);
	});
	it('serves equal priorities in arrival order', () => {
		const jobs = [{ id: 'a', priority: 2 }, { id: 'b', priority: 1 }, { id: 'c', priority: 2 }, { id: 'd', priority: 1 }]
			.map((j, i) => ({ ...j, source: 'pq', at: 100 + 10 * i, work: 2500 }));
		const ev = simulatePool(plan({ limit: 4, sources: [{ id: 'pq', priority: true }], jobs }));
		assert.deepEqual(dispatches(ev).map(e => e.job), ['b', 'd', 'a', 'c']);
	});
	it('puts a control before a land at the same instant', () => {
		const p = plan({ jobs: [{ id: 'x', source: 'q', at: 100, work: 500 }], controls: [{ at: 100, op: 'pause' }] });
		assert.deepEqual(simulatePool(p).map(e => e.type), ['control', 'land']);
	});
	it('lets a worker at limit 1 take one waiting job at a time', () => {
		const jobs = [100, 1000, 3500].map((at, i) => ({ id: `j${i}`, source: 'q', at, work: 2000 }));
		const ev = simulatePool(plan({ limit: 1, jobs }));
		assert.deepEqual(dispatches(ev).map(e => [e.at, e.job, e.waiting]), [[500, 'j0', 0], [2800, 'j1', 0], [5100, 'j2', 0]]);
	});
	it('wakes the loop wake after a resume or a tune', () => {
		const late = { id: 'late', source: 'q', at: 3000, work: 500 };
		const resumed = plan({ jobs: [...burst(2), late], controls: [{ at: 2500, op: 'pause' }, { at: 6000, op: 'resume' }] });
		assert.deepEqual(dispatches(simulatePool(resumed)).filter(e => e.job === 'late').map(e => e.at), [6400]);
		const tuned = plan({ limit: 2, jobs: burst(4, 4000), controls: [{ at: 1000, op: 'tune', to: 4 }] });
		const ev = simulatePool(tuned);
		assert.deepEqual(dispatches(ev).map(e => [e.at, e.job]), [[500, 'j0'], [950, 'j1'], [1400, 'j2'], [1850, 'j3']]);
		assert.deepEqual(ev.filter(e => e.type === 'control'), [{ at: 1000, type: 'control', op: 'tune', to: 4 }]);
	});
	it('stop with nothing running sends the idle goroutines away at once, the last to go idle first', () => {
		const p = plan({ idle: [0, 1], jobs: burst(1), controls: [{ at: 50, op: 'stop' }] });
		const ev = simulatePool(p);
		assert.deepEqual(ev, [
			{ at: 50, type: 'control', op: 'stop' }, { at: 50, type: 'exit', ring: 1 }, { at: 50, type: 'exit', ring: 0 },
			{ at: 100, type: 'land', job: 'j0', source: 'q' },
		]); // and the job that lands afterwards is never dispatched
		assert.deepEqual(endState(p, ev).idle, []);
	});
	it('stop keeps an idle goroutine until the last running job is done', () => {
		// j0 runs on ring 1, the last to go idle. Ring 0 sits idle through the stop at 1000 and leaves with it at 2500.
		const jobs = [{ id: 'j0', source: 'q', at: 100, work: 2000 }];
		const p = plan({ idle: [0, 1], jobs, controls: [{ at: 1000, op: 'stop' }] });
		const ev = simulatePool(p);
		assert.deepEqual(ev.filter(e => e.type === 'exit'), [{ at: 2500, type: 'exit', ring: 1 }, { at: 2500, type: 'exit', ring: 0 }]);
		assert.deepEqual(endState(p, ev).idle, []);
	});
	it('stop applies the usual stay-or-exit rule to a goroutine that finishes, and the rest leave with the last job', () => {
		// Three jobs run on rings 0, 1, 2 when the stop comes, and nothing waits. Ring 0 finishes with none idle, so it
		// stays. Ring 1 finishes with ring 0 idle, so it exits at its own done. Ring 2 is last: it and ring 0 leave.
		const ev = simulatePool(plan({ jobs: burst(3), controls: [{ at: 1500, op: 'stop' }] }));
		assert.deepEqual(ev.filter(e => e.at > 1500), [
			{ at: 2500, type: 'done', job: 'j0', ring: 0 }, { at: 2500, type: 'idle', ring: 0 },
			{ at: 2950, type: 'done', job: 'j1', ring: 1 }, { at: 2950, type: 'exit', ring: 1 },
			{ at: 3400, type: 'done', job: 'j2', ring: 2 }, { at: 3400, type: 'exit', ring: 2 }, { at: 3400, type: 'exit', ring: 0 },
		]);
	});
	it('stop keeps a goroutine that finishes idle while more jobs wait than goroutines sit idle', () => {
		// The same three jobs, but j3 and j4 are still queued at the stop and never start. Two jobs wait, so ring 1
		// stays beside ring 0, and all three leave together when the last job is done, ring 2 first.
		const ev = simulatePool(plan({ jobs: burst(5), controls: [{ at: 1500, op: 'stop' }] }));
		assert.deepEqual(ev.filter(e => e.at > 1500), [
			{ at: 2500, type: 'done', job: 'j0', ring: 0 }, { at: 2500, type: 'idle', ring: 0 },
			{ at: 2950, type: 'done', job: 'j1', ring: 1 }, { at: 2950, type: 'idle', ring: 1 },
			{ at: 3400, type: 'done', job: 'j2', ring: 2 },
			{ at: 3400, type: 'exit', ring: 2 }, { at: 3400, type: 'exit', ring: 1 }, { at: 3400, type: 'exit', ring: 0 },
		]);
	});
	it('refuses the calls varmq refuses', () => {
		// j0 runs from 500 to 2500, so the worker is still pausing at 1500 and Resume returns ErrWorkerPausing.
		const pausing = [{ at: 1000, op: 'pause' }, { at: 1500, op: 'resume' }];
		assert.throws(() => simulatePool(plan({ jobs: burst(1), controls: pausing })), /resume/);
		assert.throws(() => simulatePool(plan({ controls: [{ at: 1000, op: 'pause' }, { at: 1500, op: 'tune', to: 5 }] })), /tune/);
		for (const to of [3, 2]) assert.throws(() => simulatePool(plan({ controls: [{ at: 50, op: 'tune', to }] })), /tune/);
	});
	it('refuses several sources without a strategy, which varmq would read by priority', () => {
		assert.throws(() => simulatePool(plan({ sources: [{ id: 'a' }, { id: 'b' }] })), /strategy/);
		assert.deepEqual(simulatePool(plan({ strategy: 'roundrobin' })), []); // one source needs none, but may name one
	});
});

describe('poolStates and endState', () => {
	it('give one state per event, with each source head first', () => {
		const jobs = [5, 2, 8, 1, 3].map((pr, i) => ({ id: `p${pr}`, source: 'pq', at: 100 + 50 * i, work: 2500, priority: pr }));
		const p = plan({ sources: [{ id: 'pq', priority: true }], jobs });
		const ev = simulatePool(p);
		const states = poolStates(p, ev);
		assert.equal(states.length, ev.length);
		assert.deepEqual(states[0], { at: 100, busy: [], idle: [0], waiting: { pq: ['p5'] } });
		assert.deepEqual(states[4], { at: 300, busy: [], idle: [0], waiting: { pq: ['p1', 'p2', 'p3', 'p5', 'p8'] } });
		assert.deepEqual(states[5], { at: 500, busy: [0], idle: [], waiting: { pq: ['p2', 'p3', 'p5', 'p8'] } });
		// Each state is its own copy: the idle goroutines that come later do not show up in it.
		assert.deepEqual(states[7], { at: 1400, busy: [0, 1, 2], idle: [], waiting: { pq: ['p5', 'p8'] } });
		// p1 is done at 3000: its ring is in neither list until the idle event that follows.
		assert.deepEqual([states[8], states[9]].map(s => [s.at, s.busy, s.idle]), [[3000, [1, 2], []], [3000, [1, 2], [0]]]);
	});
	it('end with the limit the last tune set and the idle rings in pool order', () => {
		const tuned = plan({ controls: [{ at: 50, op: 'tune', to: 5 }], jobs: burst(5, 4000) });
		assert.equal(endState(tuned, simulatePool(tuned)).limit, 5);
		const quiet = plan({ limit: 2, idle: [1, 0] });
		assert.deepEqual(endState(quiet, simulatePool(quiet)), { limit: 2, idle: [1, 0] });
	});
});
