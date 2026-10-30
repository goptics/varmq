import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { nextRanked, pick, type Source, type StrategyName } from './model.ts';

// The queues of the strategies page: alerts (rank 1), reports (rank 2), emails (no rank).
function order(strategy: StrategyName, lens: [number, number, number]): string[] {
	const names = ['alert', 'report', 'email'];
	const src: Source[] = names.map((name, i) => ({ name, len: lens[i] }));
	const rr = { i: 0 };
	const out: string[] = [];
	for (;;) {
		const p = pick(strategy, src, rr);
		if (!p) return out;
		src[p.index].len--;
		out.push(names[p.index]);
	}
}

describe('strategies', () => {
	it('Priority takes the first queue in the list that has a job', () => {
		assert.deepEqual(order('Priority', [2, 1, 3]), ['alert', 'alert', 'report', 'email', 'email', 'email']);
	});
	it('RoundRobin takes one job from each queue in turn and skips an empty queue', () => {
		assert.deepEqual(order('RoundRobin', [2, 1, 3]), ['alert', 'report', 'email', 'alert', 'email', 'email']);
	});
	it('MaxLen breaks a tie with the queue that comes first', () => {
		assert.deepEqual(order('MaxLen', [2, 1, 3]), ['email', 'alert', 'email', 'alert', 'report', 'email']);
	});
	it('MinLen empties the shortest queue first and ignores empty queues', () => {
		assert.deepEqual(order('MinLen', [2, 1, 3]), ['report', 'alert', 'alert', 'email', 'email', 'email']);
	});
	it('returns null when every queue is empty', () => {
		assert.equal(pick('MaxLen', [{ name: 'a', len: 0 }], { i: 0 }), null);
	});
});

describe('priority queue', () => {
	it('runs the lowest number first and keeps the add order for equal numbers', () => {
		const jobs = [
			{ name: 'report', priority: 10, seq: 1 },
			{ name: 'alert', priority: 1, seq: 2 },
			{ name: 'digest', priority: 10, seq: 3 },
		];
		const out: string[] = [];
		for (let j = nextRanked(jobs); j; j = nextRanked(jobs)) {
			out.push(j.name);
			jobs.splice(jobs.indexOf(j), 1);
		}
		assert.deepEqual(out, ['alert', 'report', 'digest']);
	});
});
