import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { PRIORITY_ADDS, RR_QUEUES, priorityQueue, roundRobin } from './order.ts';

describe('home scenes: job order', () => {
	it('runs the lowest number first, and equal numbers in the order added', () => {
		const states = priorityQueue([
			{ name: 'report', priority: 10 },
			{ name: 'alert', priority: 1 },
			{ name: 'digest', priority: 10 },
		]);
		assert.deepEqual(
			states.map((s) => s.map((a) => a.name)),
			[['report'], ['alert', 'report'], ['alert', 'report', 'digest']],
		);
	});

	it('takes turns under RoundRobin, passing over empty queues', () => {
		const picks = roundRobin([['a1', 'a2'], ['r1'], ['e1', 'e2', 'e3']]);
		assert.deepEqual(
			picks.map((p) => p.job),
			['a1', 'r1', 'e1', 'a2', 'e2', 'e3'],
		);
		assert.deepEqual(
			picks.map((p) => p.skipped),
			[[], [], [], [], [1], [0, 1]],
		);
	});

	it('takes nothing from empty queues', () => {
		assert.deepEqual(roundRobin([[], []]), []);
	});

	it('plays the programs on the page', () => {
		// The priority scene adds what examples/queues-priority adds, and the RoundRobin scene's queues hold what
		// examples/strategies-round-robin adds: alerts, reports, emails.
		assert.deepEqual(PRIORITY_ADDS, [
			{ name: 'report', priority: 10 },
			{ name: 'alert', priority: 1 },
			{ name: 'digest', priority: 10 },
		]);
		assert.deepEqual(RR_QUEUES.map((q) => q.jobs.length), [2, 1, 3]);
		assert.deepEqual(
			roundRobin(RR_QUEUES.map((q) => q.jobs)).map((p) => p.job),
			['a1', 'r1', 'e1', 'a2', 'e2', 'e3'],
		);
	});
});
