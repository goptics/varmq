import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { describe, it } from 'node:test';
import { againstVarmq, homeCharts, latencyP50 } from './home.ts';
import type { Data } from './lib.ts';

const data = JSON.parse(readFileSync(new URL('data.json', import.meta.url), 'utf8')) as Data;
const charts = homeCharts(data);
const value = (lanes: { subject: string; value: number }[], s: string) => lanes.find((l) => l.subject === s)!.value;
const round = (v: number, d: number) => Number(v.toFixed(d));

describe('home page Performance figures', () => {
	it('averages the pool over 45 groups with every subject in each', () => {
		for (const c of charts.pool) {
			assert.equal(c.groups, 45, c.id);
			assert.deepEqual(c.partial, [], c.id);
			assert.equal(c.lanes.length, 9, c.id);
		}
	});

	it('states the pool throughput the page states: 4.89M, 2.73M, 2.22M tasks/s', () => {
		const l = charts.pool[0].lanes;
		assert.equal(l[0].subject, 'VarMQ');
		assert.equal(round(value(l, 'VarMQ') / 1e6, 2), 4.89);
		assert.equal(round(value(l, 'BufferedPool') / 1e6, 2), 2.73);
		assert.equal(round(value(l, 'Ants') / 1e6, 2), 2.22);
		assert.equal(Math.round(value(l, 'VarMQ')), 4892786);
	});

	it('states the pool memory: VarMQ 115.0 MB, ants 15.4 MB per 1M tasks', () => {
		const l = charts.pool[1].lanes;
		assert.equal(round(value(l, 'VarMQ'), 1), 115.0);
		assert.equal(round(value(l, 'Ants'), 1), 15.4);
	});

	it('states Redis: 59,898 jobs/s with 100 producers, Rmq 35,708 against 20,812 with one', () => {
		const [many, one] = charts.redis;
		assert.equal(many.lanes[0].subject, 'VarMQ');
		assert.equal(value(many.lanes, 'VarMQ'), 59898);
		assert.equal(one.lanes[0].subject, 'Rmq');
		assert.equal(value(one.lanes, 'Rmq'), 35708);
		assert.equal(value(one.lanes, 'VarMQ'), 20812);
		for (const c of charts.redis) assert.ok(!c.lanes.some((l) => l.subject === 'VarMQDistributed'), c.id);
		assert.deepEqual(latencyP50(data, 'VarMQ'), { value: 0.05779, lowest: true });
	});

	it('states SQLite: VarMQ 14,725, goqite 2,933, River 3,230 jobs/s', () => {
		const l = charts.sqlite[0].lanes;
		assert.equal(value(l, 'VarMQ'), 14725);
		assert.equal(value(l, 'Goqite'), 2933);
		assert.equal(value(l, 'River'), 3230);
	});

	it('backs every line of the callout against VarMQ', () => {
		const a = againstVarmq(data);
		assert.equal(round(a.memory.ratio, 1), 7.5);
		assert.deepEqual(a.oneWorker, { groups: 15, buffered: 15 });
		assert.deepEqual(a.cpu4, { groups: 55, varmq: 19, buffered: 21 });
		assert.deepEqual(a.cpu24, { groups: 55, varmq: 39, buffered: 12 });
		assert.deepEqual(a.redisOne, { groups: 13, rmq: 12, varmqEnqueue: true });
		assert.deepEqual(a.redisManyWorkers, ['100', '1000']);
		assert.deepEqual(a.sqlite, { groups: 8, varmq: 8 });
	});
});
