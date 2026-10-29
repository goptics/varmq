import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { stepsOf } from './ack-data.ts';
import { PRESETS, behaviours, type Caps } from './adapter-data.ts';
import { CPU, handle, seed, tick, type Worker } from './api-model.ts';
import { close, fresh, kill, nacked, sweepIfDue, take } from './redisflow-model.ts';

describe('crash test steps', () => {
	it('show no acknowledgement for a queue in memory', () => {
		const steps = stepsOf('memory');
		assert.equal(steps.length, 5);
		for (const s of steps) assert.doesNotMatch(`${s.sub} ${s.call}`, /Ack|JSON/, s.label);
		assert.equal(steps[1].sub, 'Dequeue');
	});

	it('take jobs from redisq and sqliteq with one batch call, and from your adapter with DequeueWithAckId', () => {
		for (const mode of ['redis', 'dist', 'sqlite'] as const) assert.equal(stepsOf(mode)[1].sub, 'DequeueBatchWithAckIds', mode);
		assert.equal(stepsOf('own')[1].sub, 'DequeueWithAckId');
		for (const mode of ['redis', 'dist', 'sqlite', 'own'] as const) assert.equal(stepsOf(mode)[3].sub, 'Acknowledge', mode);
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

describe('API model', () => {
	const run = (method: string, path: string, workers = seed()) => ({ reply: handle(workers, method, path), workers });

	it('gives the plain 404 of the mux for an empty {name} segment', () => {
		const { reply } = run('GET', '/varmq/workers/');
		assert.equal(reply.code, 404);
		assert.equal(reply.json, false);
		assert.equal(reply.body, '404 page not found');
		assert.ok(reply.headers.some(([k]) => k === 'X-Varmq-Instance'));
	});

	it('answers 400 for a concurrency outside the int range, and accepts the largest int', () => {
		const path = '/varmq/workers/email-sender/config/concurrency/';
		assert.equal(run('PATCH', path + '99999999999999999999').reply.code, 400);
		assert.equal(run('PATCH', path + '-9223372036854775809').reply.code, 400);
		assert.equal(run('PATCH', path + 'abc').reply.code, 400);
		const big = run('PATCH', path + '9223372036854775807');
		assert.equal(big.reply.code, 200);
		assert.equal(big.workers.get('email-sender')!.concurrency, 4294967295, 'clamped to MaxUint32');
		assert.equal(run('PATCH', path + '+3').reply.code, 200);
		const zero = run('PATCH', path + '0');
		assert.equal(zero.workers.get('email-sender')!.concurrency, CPU);
	});

	it('keeps the replies the buttons were checked against', () => {
		const workers = seed();
		const w = workers.get('email-sender') as Worker;
		assert.equal(handle(workers, 'PATCH', '/varmq/workers/email-sender/actions/pause').code, 200);
		assert.equal(w.status, 'Pausing');
		const resume = handle(workers, 'PATCH', '/varmq/workers/email-sender/actions/resume');
		assert.deepEqual([resume.code, resume.body], [500, '{"error":"worker is pausing"}']);
		tick(w);
		assert.equal(w.status, 'Paused');
		assert.equal(handle(workers, 'PATCH', '/varmq/workers/email-sender/actions/resume').code, 200);
		assert.equal(handle(workers, 'PATCH', '/varmq/workers/email-sender/actions/start').code, 409);
		const allow = handle(workers, 'POST', '/varmq/health');
		assert.equal(allow.code, 405);
		assert.deepEqual(allow.headers[0], ['Allow', 'GET, HEAD']);
		assert.equal(handle(workers, 'GET', '/varmq/workers/ghost').body, '{"error":"worker not found"}');
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
