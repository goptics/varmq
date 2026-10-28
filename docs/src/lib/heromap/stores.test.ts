import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { queueStates, simulateRedis, simulateSqlite, storeEnd, trayStates } from './stores.ts';

const sqlite = { limit: 2, idle: [0], jobs: ['s0', 's1', 's2', 's3'].map((id, i) => ({ id, at: 300 + 260 * i, work: [4200, 1050, 3000, 1100][i] })),
	firstClaim: 2300, crashAt: 6600, restartAt: 8000, requeueAfter: 650, rework: { s0: 1700, s2: 1200, s3: 1100 }, takeTravel: 720, ackTravel: 780 };
const redis = { consumers: [{ id: 'b', limit: 2, idle: [0] }, { id: 'c', limit: 2, idle: [0] }], jobs: [0, 1, 2, 3, 4, 5].map(i => ({ id: `r${i}`, at: 300 + 240 * i, work: [2200, 1900, 2500, 2100, 1700, 1900][i] })),
	landAfter: 700, notifyTravel: 420, takeTravel: 720, ackTravel: 730 };
const takes = ev => ev.filter(e => e.type === 'take');

describe('sqlite', () => {
	const ev = simulateSqlite(sqlite);
	it('claims one job per free ring in one statement', () => {
		const first = takes(ev).filter(e => e.at === takes(ev)[0].at);
		assert.deepEqual(first.map(e => e.job), ['s0', 's1']);
	});
	it('takes the next job only after the ack lands', () => {
		for (const t of takes(ev)) {
			const prev = ev.filter(e => e.type === 'ack' && e.at <= t.at);
			const ringDone = ev.filter(e => e.type === 'done' && e.ring === t.ring && e.at < t.at);
			for (const d of ringDone) assert.ok(prev.some(a => a.job === d.job), `${t.job} before ack of ${d.job}`);
		}
	});
	it('puts crashed jobs back at the head after the restart', () => {
		const rq = ev.find(e => e.type === 'requeue');
		assert.deepEqual(rq.jobs, ['s0', 's2']);
		assert.deepEqual(queueStates(ev).find(s => s.at === rq.at).order, ['s0', 's2', 's3']);
	});
	it('acks every job by the end', () => {
		assert.deepEqual(ev.filter(e => e.type === 'ack').map(e => e.job).sort(), ['s0', 's1', 's2', 's3']);
	});
});

describe('redis', () => {
	const ev = simulateRedis(redis);
	it('gives each job to exactly one consumer', () => {
		assert.deepEqual(takes(ev).map(e => e.job).sort(), ['r0', 'r1', 'r2', 'r3', 'r4', 'r5']);
	});
	it('notifies every consumer of every land', () => {
		assert.equal(ev.filter(e => e.type === 'notify').length, 6 * 2);
	});
	it('alternates the first four takes between b and c', () => {
		assert.deepEqual(takes(ev).slice(0, 4).map(e => e.consumer), ['b', 'c', 'b', 'c']);
	});
});

describe('trays', () => {
	for (const [name, ev, max] of [['sqlite', simulateSqlite(sqlite), 2], ['redis', simulateRedis(redis), 4]] as const) {
		it(`${name}: in flight on distinct slots, never more than ${max}`, () => {
			for (const s of trayStates(ev)) {
				const held = s.tray.filter(Boolean);
				assert.equal(new Set(held).size, held.length);
				assert.ok(held.length <= max, `${held.length} at ${s.at}`);
			}
		});
	}
});

// What the tests above leave open: the exact timeline of each fixture, the rules behind it, and the replays.
// An event on one line: time, type, then the job (or jobs), consumer, ring and tray slot it carries.
const show = ev => ev.map(e => [e.at, e.type, e.job ?? e.jobs?.join(','), e.consumer,
	e.ring === undefined ? undefined : `ring ${e.ring}`, e.tray === undefined ? undefined : `tray ${e.tray}`]
	.filter(x => x !== undefined && x !== '').join(' '));
const sq = p => ({ limit: 2, idle: [0], jobs: [], firstClaim: 1000, crashAt: 90000, restartAt: 95000, requeueAfter: 500, rework: {},
	takeTravel: 100, ackTravel: 50, ...p });
const rd = p => ({ consumers: [{ id: 'b', limit: 2, idle: [0] }, { id: 'c', limit: 2, idle: [0] }], jobs: [], landAfter: 100, notifyTravel: 100,
	takeTravel: 50, ackTravel: 50, ...p });
const burst = (ids, at = 0, work = 500) => ids.map((id, i) => ({ id, at: at + 10 * i, work }));

describe('sqlite rules', () => {
	it('plays the fixture out exactly', () => {
		assert.deepEqual(show(simulateSqlite(sqlite)), [
			'300 land s0', '560 land s1', '820 land s2', '1080 land s3',
			// One statement claims both: s0 goes to the idle ring 0, and a goroutine starts on ring 1 for s1. s1 is done
			// 720 + 1050 later, and its ack frees slot 1, and ring 1 stays, for s2.
			'2300 take s0 sqlite ring 0 tray 0', '2300 spawn sqlite ring 1', '2300 take s1 sqlite ring 1 tray 1',
			'4070 done s1 sqlite ring 1', '4850 ack s1 tray 1', '4850 take s2 sqlite ring 1 tray 1',
			// s0 and s2 are still running when the program dies, so they never get a done or an ack. Its goroutines go
			// with it, and it restarts with one.
			'6600 crash', '6600 exit sqlite ring 0', '6600 exit sqlite ring 1', '8000 restart', '8000 spawn sqlite ring 0',
			// The requeue puts them at the head, a claim follows at once, and they run again for their rework time.
			'8650 requeue s0,s2', '8650 take s0 sqlite ring 0 tray 0', '8650 spawn sqlite ring 1', '8650 take s2 sqlite ring 1 tray 1',
			'10570 done s2 sqlite ring 1', '11070 done s0 sqlite ring 0',
			'11350 ack s2 tray 1', '11350 take s3 sqlite ring 1 tray 1', '11850 ack s0 tray 0',
			// Nothing waits once s3 is acked, and ring 0 is idle, so ring 1 exits.
			'13170 done s3 sqlite ring 1', '13950 ack s3 tray 1', '13950 exit sqlite ring 1',
		]);
	});
	it('claims for every ring that is free at the same instant in one statement', () => {
		// a and b run side by side and are acked together, so c and d go in one claim, after both acks: c to ring 1,
		// the last to go idle
		const ev = simulateSqlite(sq({ jobs: burst(['a', 'b', 'c', 'd'], 100) }));
		assert.deepEqual(show(ev.filter(e => e.at > 1000 && e.at < 2000)), [
			'1600 done a sqlite ring 0', '1600 done b sqlite ring 1', '1650 ack a tray 0', '1650 ack b tray 1',
			'1650 take c sqlite ring 1 tray 0', '1650 take d sqlite ring 0 tray 1',
		]);
	});
	it('claims a job that lands at the first claim with the burst', () => {
		const ev = simulateSqlite(sq({ jobs: [{ id: 'a', at: 100, work: 300 }, { id: 'b', at: 1000, work: 300 }] }));
		assert.deepEqual(show(ev.slice(0, 5)), ['100 land a', '1000 land b', '1000 take a sqlite ring 0 tray 0',
			'1000 spawn sqlite ring 1', '1000 take b sqlite ring 1 tray 1']);
	});
	it('claims a job that lands after the first claim as soon as it lands, while a ring is free', () => {
		// x waits for the first claim. y lands at 1500, when x is acked (1450) and both rings are free.
		const ev = simulateSqlite(sq({ jobs: [{ id: 'x', at: 100, work: 300 }, { id: 'y', at: 1500, work: 300 }] }));
		assert.deepEqual(show(ev.filter(e => e.at < 90000)), [
			'100 land x', '1000 take x sqlite ring 0 tray 0', '1400 done x sqlite ring 0', '1450 ack x tray 0',
			'1500 land y', '1500 take y sqlite ring 0 tray 0', '1900 done y sqlite ring 0', '1950 ack y tray 0',
		]);
	});
	it('keeps the running jobs in flight from the crash to the requeue', () => {
		const ev = simulateSqlite(sqlite), tray = trayStates(ev), queue = queueStates(ev);
		const crash = ev.findIndex(e => e.type === 'crash'), requeue = ev.findIndex(e => e.type === 'requeue');
		for (const i of [crash, crash + 1]) { // the crash, and the restart
			assert.deepEqual(tray[i].tray, ['s0', 's2', null, null, null]);
			assert.deepEqual(queue[i].order, ['s3']);
		}
		assert.deepEqual(tray[requeue].tray, [null, null, null, null, null]);
	});
	it('puts crashed jobs back in the order they were written to the file', () => {
		// z is acked at 1450 and a takes its slot 0, so the tray holds a, m. Write order is m, a: sqliteq orders its rows by id.
		const jobs = [{ id: 'z', at: 100, work: 300 }, { id: 'm', at: 110, work: 5000 }, { id: 'a', at: 120, work: 5000 }];
		const ev = simulateSqlite(sq({ jobs, crashAt: 2000, restartAt: 3000 }));
		assert.deepEqual(trayStates(ev).find((s, i) => ev[i].type === 'crash').tray, ['a', 'm', null, null, null]);
		assert.deepEqual(show(ev.filter(e => e.at >= 2000 && e.at < 4000)), [
			'2000 crash', '2000 exit sqlite ring 0', '2000 exit sqlite ring 1', '3000 restart', '3000 spawn sqlite ring 0',
			'3500 requeue m,a', '3500 take m sqlite ring 0 tray 0', '3500 spawn sqlite ring 1', '3500 take a sqlite ring 1 tray 1',
		]);
	});
	it('requeues nothing when nothing is in flight at the crash', () => {
		const ev = simulateSqlite(sq({ jobs: [{ id: 'a', at: 100, work: 300 }], firstClaim: 200, crashAt: 1000, restartAt: 1500, requeueAfter: 100 }));
		assert.deepEqual(ev.slice(-5), [{ at: 1000, type: 'crash' }, { at: 1000, type: 'exit', consumer: 'sqlite', ring: 0 },
			{ at: 1500, type: 'restart' }, { at: 1500, type: 'spawn', consumer: 'sqlite', ring: 0 }, { at: 1600, type: 'requeue', jobs: [] }]);
	});
	it('does not claim while the program is down, and claims at the requeue instead', () => {
		// the first claim is due at 1500, between the crash at 1000 and the requeue at 2000
		const jobs = [{ id: 'a', at: 100, work: 300 }];
		const ev = simulateSqlite(sq({ jobs, firstClaim: 1500, crashAt: 1000, restartAt: 1200, requeueAfter: 800 }));
		assert.deepEqual(show(ev.filter(e => e.at < 90000)), [
			'100 land a', '1000 crash', '1000 exit sqlite ring 0', '1200 restart', '1200 spawn sqlite ring 0', '2000 requeue',
			'2000 take a sqlite ring 0 tray 0', '2400 done a sqlite ring 0', '2450 ack a tray 0',
		]);
	});
	it('runs a job again for its rework time, or for its work time when it has no rework entry', () => {
		const dones = ev => ev.filter(e => e.type === 'done' && e.at > 8650).map(e => [e.at, e.job]);
		assert.deepEqual(dones(simulateSqlite(sqlite)), [[10570, 's2'], [11070, 's0'], [13170, 's3']]);
		// s2 and s0 take 3000 and 4200 again, and s3 is taken once s2 is acked at 13150
		assert.deepEqual(dones(simulateSqlite({ ...sqlite, rework: {} })), [[12370, 's2'], [13570, 's0'], [14970, 's3']]);
	});
	it('refuses what the story cannot show', () => {
		const a = { id: 'a', at: 100, work: 300 };
		// a is done at 600 and its ack is on the way until 650: the crash at 620 leaves it unclear whether it landed
		assert.throws(() => simulateSqlite(sq({ jobs: [a], firstClaim: 200, crashAt: 620 })), /^Error: crash:/);
		// the program is down from the crash to the requeue, and Add() runs in the program
		const late = { id: 'b', at: 1200, work: 300 };
		assert.throws(() => simulateSqlite(sq({ jobs: [a, late], crashAt: 1000, restartAt: 1500 })), /^Error: land:/);
		// the crash comes before a land at the same instant
		assert.throws(() => simulateSqlite(sq({ jobs: [a, { id: 'c', at: 1000, work: 300 }], crashAt: 1000, restartAt: 1500 })), /^Error: land:/);
		// the tray has five slots, and a ring can hold a record
		assert.throws(() => simulateSqlite(sq({ limit: 6 })), /^Error: rings:/);
	});
	it('runs a plan with no crash as plain add, claim, ack', () => {
		// none of crashAt, restartAt, requeueAfter and rework: the one-statement scenario above, without its crash
		const plan = { limit: 2, idle: [0], jobs: burst(['a', 'b', 'c', 'd'], 100), firstClaim: 1000, takeTravel: 100, ackTravel: 50 };
		const ev = simulateSqlite(plan);
		assert.deepEqual(ev.filter(e => e.type === 'ack').map(e => e.job), ['a', 'b', 'c', 'd']);
		assert.deepEqual(ev.filter(e => ['crash', 'restart', 'requeue'].includes(e.type)), []);
		assert.deepEqual(show(ev), [
			'100 land a', '110 land b', '120 land c', '130 land d',
			'1000 take a sqlite ring 0 tray 0', '1000 spawn sqlite ring 1', '1000 take b sqlite ring 1 tray 1',
			'1600 done a sqlite ring 0', '1600 done b sqlite ring 1',
			'1650 ack a tray 0', '1650 ack b tray 1', '1650 take c sqlite ring 1 tray 0', '1650 take d sqlite ring 0 tray 1',
			'2250 done c sqlite ring 1', '2250 done d sqlite ring 0', '2300 ack c tray 0', '2300 ack d tray 1',
			'2300 exit sqlite ring 0',
		]);
	});
	it('refuses a crash that is only partly given', () => {
		const spec = { crashAt: 1300, restartAt: 2000, requeueAfter: 500, rework: {} };
		const plan = { limit: 2, idle: [0], jobs: burst(['a', 'b']), firstClaim: 1000, takeTravel: 100, ackTravel: 50 };
		for (const key of Object.keys(spec)) {
			const only = { [key]: spec[key] }, rest = Object.fromEntries(Object.entries(spec).filter(([k]) => k !== key));
			assert.throws(() => simulateSqlite({ ...plan, ...only }), /^Error: plan: .* only 1 of them/);
			assert.throws(() => simulateSqlite({ ...plan, ...rest }), /^Error: plan: .* only 3 of them/);
		}
		assert.doesNotThrow(() => simulateSqlite({ ...plan, ...spec })); // all four is a crash
	});
});

describe('redis rules', () => {
	it('plays the fixture out exactly', () => {
		assert.deepEqual(show(simulateRedis(redis)), [
			// Each job lands 700 after its add and is announced 420 later, to b and then to c. At 1420 both are woken and take in turn.
			'1000 land r0', '1240 land r1',
			'1420 notify b', '1420 notify c', '1420 take r0 b ring 0 tray 0', '1420 take r1 c ring 0 tray 1',
			// Each consumer starts with one goroutine, and starts a second for its second job.
			'1480 land r2', '1660 notify b', '1660 notify c', '1660 spawn b ring 1', '1660 take r2 b ring 1 tray 2',
			'1720 land r3', '1900 notify b', '1900 notify c', '1900 spawn c ring 1', '1900 take r3 c ring 1 tray 3',
			// All four rings are busy, so r4 and r5 wait in the queue through their notifies.
			'1960 land r4', '2140 notify b', '2140 notify c', '2200 land r5',
			'2380 notify b', '2380 notify c', '2620 notify b', '2620 notify c',
			// The first ack to land is c's, and only the consumer whose ring it frees pulls.
			'4040 done r1 c ring 0', '4340 done r0 b ring 0', '4720 done r3 c ring 1',
			'4770 ack r1 tray 1', '4770 take r4 c ring 0 tray 1', '4880 done r2 b ring 1',
			'5070 ack r0 tray 0', '5070 take r5 b ring 0 tray 0', '5450 ack r3 tray 3', '5610 ack r2 tray 2',
			// Once nothing waits, a goroutine finishing while the other is idle exits.
			'7190 done r4 c ring 0', '7690 done r5 b ring 0', '7920 ack r4 tray 1', '7920 exit c ring 0',
			'8420 ack r5 tray 0', '8420 exit b ring 0',
		]);
	});
	it('deals to the woken consumers in turn and skips one with no free ring', () => {
		// b has one ring and c has two: b takes j0, c takes j1, and b has no ring left for j2
		const ev = simulateRedis(rd({ consumers: [{ id: 'b', limit: 1, idle: [0] }, { id: 'c', limit: 2, idle: [0] }], jobs: burst(['j0', 'j1', 'j2'], 0, 1000) }));
		assert.deepEqual(show(takes(ev)), ['200 take j0 b ring 0 tray 0', '200 take j1 c ring 0 tray 1', '200 take j2 c ring 1 tray 2']);
	});
	it('keeps the turn between notifies, so jobs that arrive one at a time alternate too', () => {
		const jobs = [0, 1, 2, 3].map(i => ({ id: `j${i}`, at: 1000 * i, work: 9000 }));
		assert.deepEqual(takes(simulateRedis(rd({ jobs }))).map(e => [e.at, e.job, e.consumer, e.ring]),
			[[200, 'j0', 'b', 0], [1200, 'j1', 'c', 0], [2200, 'j2', 'b', 1], [3200, 'j3', 'c', 1]]);
	});
	it('wakes only the consumer whose ack landed, even before the notify of a job that landed meanwhile arrives', () => {
		// j1 lands at 900 and is announced at 1200. b's ack of j0 lands at 1000, and b takes j1 then: c has a free ring and the
		// turn, but nothing has woken it.
		const jobs = [{ id: 'j0', at: 0, work: 500 }, { id: 'j1', at: 800, work: 500 }];
		const ev = simulateRedis(rd({ consumers: [{ id: 'b', limit: 1, idle: [0] }, { id: 'c', limit: 1, idle: [0] }], jobs, notifyTravel: 300 }));
		assert.deepEqual(show(ev), [
			'100 land j0', '400 notify b', '400 notify c', '400 take j0 b ring 0 tray 0',
			'900 land j1', '950 done j0 b ring 0', '1000 ack j0 tray 0', '1000 take j1 b ring 0 tray 0',
			'1200 notify b', '1200 notify c', // they find the queue empty
			'1550 done j1 b ring 0', '1600 ack j1 tray 0',
		]);
	});
	it('takes again on a ring only after its ack has landed', () => {
		const ev = simulateRedis(redis);
		for (const t of takes(ev)) {
			const before = ev.filter(e => e.type === 'done' && e.consumer === t.consumer && e.ring === t.ring && e.at < t.at);
			for (const d of before) assert.ok(ev.some(a => a.type === 'ack' && a.job === d.job && a.at <= t.at), `${t.job} before ack of ${d.job}`);
		}
	});
	it('refuses more rings than the tray has slots', () => {
		assert.throws(() => simulateRedis(rd({ consumers: [{ id: 'b', limit: 3, idle: [0] }, { id: 'c', limit: 3, idle: [0] }] })), /^Error: rings:/);
	});
});

describe('store rules', () => {
	const fixtures = [['sqlite', sqlite, simulateSqlite], ['redis', redis, simulateRedis]] as const;
	for (const [name, plan, simulate] of fixtures) {
		it(`${name}: events come in time order`, () => {
			const at = simulate(plan).map(e => e.at);
			assert.deepEqual(at, [...at].sort((a, b) => a - b));
		});
		it(`${name}: ends with the queue and the tray empty`, () => {
			const ev = simulate(plan);
			assert.deepEqual(queueStates(ev).at(-1).order, []);
			assert.deepEqual(trayStates(ev).at(-1).tray, [null, null, null, null, null]);
		});
		it(`${name}: ends with one idle goroutine per worker`, () => {
			for (const idle of Object.values(storeEnd(plan, simulate(plan)))) assert.equal(idle.length, 1);
		});
		it(`${name}: leaves its plan as it found it`, () => {
			const before = structuredClone(plan);
			simulate(plan);
			assert.deepEqual(plan, before);
		});
	}
	it('emits exactly the fields the brief names', () => {
		const s = simulateSqlite(sqlite), r = simulateRedis(redis), one = (ev, type) => ev.find(e => e.type === type);
		assert.deepEqual(one(s, 'land'), { at: 300, type: 'land', job: 's0' });
		assert.deepEqual(one(s, 'take'), { at: 2300, type: 'take', job: 's0', consumer: 'sqlite', ring: 0, tray: 0 });
		assert.deepEqual(one(s, 'done'), { at: 4070, type: 'done', job: 's1', consumer: 'sqlite', ring: 1 });
		assert.deepEqual(one(s, 'ack'), { at: 4850, type: 'ack', job: 's1', tray: 1 });
		assert.deepEqual(one(s, 'crash'), { at: 6600, type: 'crash' });
		assert.deepEqual(one(s, 'restart'), { at: 8000, type: 'restart' });
		assert.deepEqual(one(s, 'requeue'), { at: 8650, type: 'requeue', jobs: ['s0', 's2'] });
		assert.deepEqual(one(r, 'notify'), { at: 1420, type: 'notify', consumer: 'b' });
		assert.deepEqual(one(r, 'take'), { at: 1420, type: 'take', job: 'r0', consumer: 'b', ring: 0, tray: 0 });
	});
});

describe('trayStates and queueStates', () => {
	it('give one state per event, each its own copy, with the queue head first', () => {
		const ev = simulateSqlite(sqlite), queue = queueStates(ev), tray = trayStates(ev);
		assert.equal(queue.length, ev.length);
		assert.equal(tray.length, ev.length);
		assert.deepEqual(queue[3], { at: 1080, order: ['s0', 's1', 's2', 's3'] });
		// the spawn between the two takes (event 5) changes neither
		assert.deepEqual([queue[4], queue[6]].map(s => s.order), [['s1', 's2', 's3'], ['s2', 's3']]);
		assert.deepEqual([tray[4], tray[6]], [
			{ at: 2300, tray: ['s0', null, null, null, null] }, { at: 2300, tray: ['s0', 's1', null, null, null] }]);
		// the ack at 4850 frees slot 1, and the take that follows it reuses that slot
		assert.deepEqual([tray[8], tray[9]].map(s => s.tray), [['s0', null, null, null, null], ['s0', 's2', null, null, null]]);
	});
	it('refuse an event stream the stores cannot produce', () => {
		const land = job => ({ at: 0, type: 'land', job });
		const take = (job, tray) => ({ at: 1, type: 'take', job, consumer: 'x', ring: 0, tray });
		assert.throws(() => trayStates([land('a'), land('b'), take('a', 0), take('b', 0)]), /slot 0/); // two records on one slot
		assert.throws(() => queueStates([take('a', 0)]), /not waiting/); // a take of a job that is not in the queue
		assert.throws(() => trayStates([land('a'), take('a', 0), { at: 2, type: 'ack', job: 'b', tray: 0 }]), /ack/); // an ack of another record
		assert.throws(() => queueStates([{ at: 2, type: 'requeue', jobs: ['a'] }]), /in flight/); // a requeue of a record that is not in flight
	});
});
