import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { stepsOf } from './ack-data.ts';
import { PRESETS, behaviours, type Caps } from './adapter-data.ts';
import { close, fresh, kill, nacked, sweepIfDue, take } from './redisflow-model.ts';

describe('crash test steps', () => {
	it('show no acknowledgement for a queue in memory', () => {
		const steps = stepsOf('memory');
		assert.equal(steps.length, 4);
		for (const s of steps) assert.doesNotMatch(`${s.sub} ${s.call}`, /Ack|JSON/, s.label);
		assert.equal(steps[1].sub, 'Dequeue');
	});

	it('take jobs from redisq and sqliteq with one batch call, and from your adapter with DequeueWithAckId', () => {
		for (const mode of ['redis', 'sqlite'] as const) assert.equal(stepsOf(mode)[1].sub, 'DequeueBatchWithAckIds', mode);
		assert.equal(stepsOf('own')[1].sub, 'DequeueWithAckId');
		for (const mode of ['redis', 'sqlite', 'own'] as const) {
			assert.equal(stepsOf(mode)[3].sub, 'not acknowledged', mode);
			assert.equal(stepsOf(mode)[4].sub, 'Acknowledge', mode);
		}
	});
});

describe('adapter map', () => {
	const wake = (c: Caps) => behaviours(c).find((b) => b.label === 'Wake-ups')!;
	const caps = (key: string) => PRESETS.find((p) => p.key === key)!.caps;

	it('says a persistent redisq or sqliteq worker checks the storage once a second', () => {
		for (const key of ['redis-p', 'sqlite']) {
			const b = wake(caps(key));
			assert.match(b.text, /once a second/, key);
			assert.ok(b.on, key);
		}
	});

	it('says a persistent queue without the batch interface learns of jobs from its own process only', () => {
		assert.match(wake({ ...caps('sqlite'), batch: false }).text, /own process only/);
	});

	it('leaves the wake-ups of a distributed queue to Subscribe', () => {
		assert.match(wake(caps('redis-d')).text, /Subscribe/);
	});
});

describe('Redis scene', () => {
	it('holds nothing in jobs:nacked when the process dies after Close, and never shows job 7 twice', () => {
		const s = fresh('persistent');
		take(s);
		close(s);
		kill(s);
		assert.equal(s.inflight, null);
		assert.equal(nacked(s), false);
		assert.deepEqual(s.jobs, [7, 8, 9, 10]);
		s.t = 60;
		sweepIfDue(s);
		assert.deepEqual(s.jobs, [7, 8, 9, 10]);
	});

	it('puts a job back once its dead holder is taken for dead', () => {
		for (const [mode, at] of [['persistent', 30], ['dist', 6]] as const) {
			const s = fresh(mode);
			take(s);
			kill(s);
			assert.ok(nacked(s), mode);
			s.t = at - 1;
			sweepIfDue(s);
			assert.ok(nacked(s), mode);
			s.t = at;
			sweepIfDue(s);
			assert.equal(nacked(s), false, mode);
			assert.deepEqual(s.jobs, [7, 8, 9, 10], mode);
		}
	});
});
