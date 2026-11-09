import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { stepsOf } from './ack-data.ts';
import { PRESETS, behaviours, type Caps } from './adapter-data.ts';

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

	it('says a sqliteq worker, with no Subscribe, checks the storage once a second', () => {
		for (const key of ['sqlite']) {
			const b = wake(caps(key));
			assert.match(b.text, /once a second/, key);
			assert.ok(b.on, key);
		}
	});

	it('says a persistent queue without the batch interface learns of jobs from its own process only', () => {
		assert.match(wake({ ...caps('sqlite'), batch: false }).text, /own process only/);
	});

	it('leaves the wake-ups of redisq to Subscribe', () => {
		assert.match(wake(caps('redis')).text, /Subscribe/);
	});
});
