import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { MAX_BATCH, Topic, nextRanked, pick, type Source, type StrategyName } from './model.ts';

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

describe('topic', () => {
	it('refuses a payload when nobody is subscribed', () => {
		const t = new Topic(['email']);
		t.unsubscribe(t.subs[0]);
		assert.deepEqual(t.publish(), { ok: false, why: 'no subscribers' });
	});
	it('gives every subscription its own job', () => {
		const t = new Topic(['email', 'stock']);
		const r = t.publish();
		assert.equal(r.ok, true);
		for (const s of t.subs) t.drain(s);
		assert.deepEqual(t.subs.map((s) => s.jobs), [[1], [1]]);
		assert.deepEqual(t.log, []);
	});
	it('keeps a payload until the slowest subscription has read it', () => {
		const t = new Topic(['email', 'stock']);
		t.subs[0].paused = true;
		t.publish();
		t.publish();
		for (const s of t.subs) t.drain(s);
		assert.deepEqual(t.log, [1, 2]);
		t.subs[0].paused = false;
		t.drain(t.subs[0]);
		assert.deepEqual(t.log, []);
	});
	it('refuses a payload at the capacity of the slowest subscription, then accepts after it reads', () => {
		const t = new Topic(['email']);
		t.capacity = 2;
		t.subs[0].paused = true;
		assert.equal(t.publish().ok, true);
		assert.equal(t.publish().ok, true);
		assert.deepEqual(t.publish(), { ok: false, why: 'full' });
		t.subs[0].paused = false;
		t.drain(t.subs[0]);
		assert.equal(t.publish().ok, true);
	});
	it('gives a late subscriber only the later payloads', () => {
		const t = new Topic(['email', 'audit']);
		t.unsubscribe(t.subs[1]);
		t.publish();
		t.publish();
		t.subscribe(t.subs[1]);
		t.publish();
		for (const s of t.subs) t.drain(s);
		assert.deepEqual(t.subs[0].jobs, [1, 2, 3]);
		assert.deepEqual(t.subs[1].jobs, [3]);
	});
	it('reads nothing while busy, so a slow subscriber fills the topic without a pause', () => {
		const t = new Topic(['email']);
		const s = t.subs[0];
		t.capacity = 1;
		assert.equal(t.publish().ok, true);
		assert.deepEqual(t.drain(s), [1]);
		s.jobs.shift(); // the goroutine starts order 1
		assert.equal(t.publish().ok, true);
		assert.deepEqual(t.drain(s, true), []);
		assert.deepEqual(t.publish(), { ok: false, why: 'full' });
		// Order 1 ends: the goroutine is free and reads order 2.
		assert.deepEqual(t.drain(s), [2]);
		assert.equal(t.publish().ok, true);
	});
	it('takes the unread payloads as one batch of at most 32, and nothing more while it holds them', () => {
		const t = new Topic(['email']);
		const s = t.subs[0];
		s.paused = true;
		for (let i = 0; i < 40; i++) t.publish();
		s.paused = false;
		assert.equal(t.drain(s).length, MAX_BATCH);
		assert.equal(t.unread(s).length, 40 - MAX_BATCH);
		assert.deepEqual(t.drain(s), []);
	});
	it('drops the unread payloads of a subscription that ends', () => {
		const t = new Topic(['email', 'stock']);
		t.subs[1].paused = true;
		t.publish();
		t.drain(t.subs[0]);
		assert.deepEqual(t.log, [1]);
		t.unsubscribe(t.subs[1]);
		assert.deepEqual(t.log, []);
	});
});
