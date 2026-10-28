// The SQLite and Redis stores as discrete-event simulations: a queue lane and an in-flight tray in the
// store, and the workers that take jobs from it. Plans in, events out, for the chapter scripts to play
// back; nothing here draws, though the tray has the five slots the scene gives it. The rules come from
// persistence/overview, sqlite and redis in these docs:
// - A taken job stays in the store as a record in flight (a `processing` row, an entry in `<key>:nacked`)
//   until its ack lands. varmq acks when the job's function returns, before the goroutine goes back to
//   the pool (`j.Close()` in `worker_pool.go`), so a ring takes its next job only after its ack, and holds
//   one record at a time.
// - A store's worker is a pool like any other varmq worker. `Start()` leaves one goroutine idle; a take
//   goes to the goroutine that went idle last, or to a new one on the lowest free ring when none is idle
//   (`dispatchCredit`); after its ack, a goroutine stays idle while more jobs wait in the queue than other
//   goroutines sit idle, or while none does, and otherwise exits (`freePoolNode`). When a program dies, its
//   goroutines go with it, and it restarts with one.
// - sqliteq: a worker with this queue alone claims one job per credit (its limit less the jobs it runs) in
//   one statement (`dispatchCredit`). Rows come out by their integer id, which is write order. Opening the queue sets
//   every `processing` row that was never acked back to `pending` (`requeueNoAck`); claims take the lowest
//   ids, so those rows sort ahead of every row still waiting, and among themselves in write order.
// - redisq v1.4.0: an add pushes the job on a list and publishes a notification that every consumer gets.
//   A woken worker pulls one job per credit, and a job one consumer has taken is not in the list for
//   another. Which consumer wins a job is a race on the Redis server; here the woken consumers take in
//   turn, one job each, as a stand-in for it. A job whose process died is requeued only once its
//   visibility timeout (five minutes by default) has passed, so there is no crash in the Redis story.
// A job runs `takeTravel` after its take (the copy's flight to the ring) and is done `work` ms later; the
// animation runs the ring's progress from arrival to done. Its ack lands `ackTravel` after that. The tray
// has five slots, and a record holds the lowest free one from its take until its ack, so a store's workers'
// limits (five at most together) bound it. Events at one instant come in the order they were set: SQLite sets the crash,
// when it has one, before the lands, so a land at the instant of the crash comes after it, and a claim or a
// pull is set by whatever wakes it, so it follows everything already due at that instant.

import { SLOTS } from './scene.ts';

export interface StoreJob {
	id: string;
	// SQLite: when its row is written, which lands it. Redis: when program A adds it, landAfter before it lands.
	at: number;
	work: number;
}

export interface SqlitePlan {
	limit: number; // the worker's concurrency: the most rings it runs at once
	idle: number[]; // its rings idle at the start, the last to go idle last: [0] after `Start()`
	jobs: StoreJob[];
	firstClaim: number; // when the worker first looks at the queue, after the burst has landed
	// The crash is all or none of crashAt, restartAt, requeueAfter and rework. With none of them the run is
	// add, claim, ack, with no crash, restart or requeue. With only some of them, simulateSqlite throws.
	crashAt?: number;
	restartAt?: number;
	requeueAfter?: number; // ms from the restart to the queue being opened: the requeue, and a claim at once
	// The work of a run after the restart. A job with no entry takes its `work` again.
	rework?: Record<string, number>;
	takeTravel: number;
	ackTravel: number;
}

/** A consumer program's worker: its concurrency, and its rings idle at the start (as `SqlitePlan` has them). */
export interface Consumer {
	id: string;
	limit: number;
	idle: number[];
}

export interface RedisPlan {
	consumers: Consumer[]; // in notify order
	jobs: StoreJob[];
	landAfter: number;
	notifyTravel: number; // ms from a land to the notification reaching a consumer
	takeTravel: number;
	ackTravel: number;
}

// A job starts running takeTravel after its take (the copy's flight to the ring):
// done = take + takeTravel + work. The ack lands ackTravel after done.
export type StoreEvent =
	| { at: number; type: 'land'; job: string } // the job is in the queue, at its tail
	| { at: number; type: 'notify'; consumer: string } // the notification reaches the consumer
	// A goroutine starts on the ring, idle: for a take with none idle (just before it), or at a restart.
	| { at: number; type: 'spawn'; consumer: string; ring: number }
	| { at: number; type: 'take'; job: string; consumer: string; ring: number; tray: number } // goes in flight
	| { at: number; type: 'done'; job: string; consumer: string; ring: number } // the function returns
	// The record is gone, and the ring's goroutine goes back to its pool, idle.
	| { at: number; type: 'ack'; job: string; tray: number }
	// The goroutine stops: just after its ack, as `freePoolNode` decides, or when its program dies.
	| { at: number; type: 'exit'; consumer: string; ring: number }
	| { at: number; type: 'crash' }
	| { at: number; type: 'restart' }
	| { at: number; type: 'requeue'; jobs: string[] }; // the records in flight, in write order, to the head

/** A worker's goroutines: the busy rings, each with the job it holds from its take until its ack, and the idle ones. */
interface Rings {
	busy: Map<number, string>;
	idle: number[]; // a stack: the ring that went idle last is at the end and takes the next job
}

interface Store {
	queue: string[];
	tray: (string | null)[];
	rings?: Map<string, Rings>; // by consumer; replays of the queue and the tray alone leave it out
}

/** The workers of a plan, the SQLite one as the consumer 'sqlite'. */
export function workers(plan: SqlitePlan | RedisPlan): Consumer[] {
	return 'consumers' in plan ? plan.consumers : [{ id: 'sqlite', limit: plan.limit, idle: plan.idle }];
}

const fresh = (plan?: SqlitePlan | RedisPlan): Store => ({
	queue: [],
	tray: Array<string | null>(SLOTS).fill(null),
	rings: plan && new Map(workers(plan).map((w) => [w.id, { busy: new Map(), idle: [...w.idle] }])),
});

const lowestFree = (taken: Iterable<number>): number => {
	const set = new Set(taken);
	let ring = 0;
	while (set.has(ring)) ring++;
	return ring;
};

/**
 * `apply`, the one place an event changes a store. The simulations run every event they emit through it,
 * and `queueStates` and `trayStates` replay events through it, so what a simulation decides on and what
 * the replays report cannot drift apart. It refuses an event no store can produce, so a replay cannot
 * hide one.
 */
function apply(s: Store, e: StoreEvent): void {
	switch (e.type) {
		case 'land':
			s.queue.push(e.job);
			break;
		case 'spawn': {
			const r = s.rings?.get(e.consumer);
			if (r && (r.busy.has(e.ring) || r.idle.includes(e.ring))) {
				throw new Error(`spawn: ring ${e.ring} of ${e.consumer} has a goroutine at ${e.at} ms`);
			}
			r?.idle.push(e.ring);
			break;
		}
		case 'take': {
			const i = s.queue.indexOf(e.job);
			if (i < 0) throw new Error(`take: ${e.job} is not waiting at ${e.at} ms`);
			if (s.tray[e.tray] !== null) {
				throw new Error(`take: slot ${e.tray} is not free for ${e.job} at ${e.at} ms`);
			}
			s.queue.splice(i, 1);
			s.tray[e.tray] = e.job;
			const r = s.rings?.get(e.consumer);
			if (r) {
				const k = r.idle.indexOf(e.ring);
				if (k < 0) throw new Error(`take: ring ${e.ring} of ${e.consumer} is not idle for ${e.job} at ${e.at} ms`);
				r.idle.splice(k, 1);
				r.busy.set(e.ring, e.job);
			}
			break;
		}
		case 'ack':
			if (s.tray[e.tray] !== e.job) {
				throw new Error(`ack: slot ${e.tray} does not hold ${e.job} at ${e.at} ms`);
			}
			s.tray[e.tray] = null;
			for (const r of s.rings?.values() ?? []) {
				for (const [ring, job] of r.busy) {
					if (job !== e.job) continue;
					r.busy.delete(ring);
					r.idle.push(ring);
				}
			}
			break;
		case 'exit': {
			const r = s.rings?.get(e.consumer);
			if (!r) break;
			const k = r.idle.indexOf(e.ring);
			if (!r.busy.delete(e.ring) && k < 0) {
				throw new Error(`exit: ring ${e.ring} of ${e.consumer} has no goroutine at ${e.at} ms`);
			}
			if (k >= 0) r.idle.splice(k, 1);
			break;
		}
		case 'requeue':
			for (const job of e.jobs) {
				const i = s.tray.indexOf(job);
				if (i < 0) throw new Error(`requeue: ${job} is not in flight at ${e.at} ms`);
				s.tray[i] = null;
			}
			s.queue.unshift(...e.jobs);
			break;
		// notify, done, crash and restart change none of the queue, the tray and the rings
	}
}

/** One program's worker. Its rings are in the store (`Store.rings`), so the replays see what it decides on. */
interface Worker {
	id: string;
	limit: number;
	deaths: number; // what a ring was going to do before its program last died never happens
}

const tooManyRings = (rings: number) =>
	new Error(`rings: ${rings} rings can hold ${rings} records in flight, and the tray has ${SLOTS} slots`);

function engine(plan: (SqlitePlan | RedisPlan) & { takeTravel: number; ackTravel: number }) {
	const store = fresh(plan);
	const rings = (w: Worker) => store.rings!.get(w.id)!;
	const events: StoreEvent[] = [];
	const timers: { at: number; seq: number; fire: (now: number) => void }[] = [];
	let seq = 0;

	const emit = (e: StoreEvent) => {
		events.push(e);
		apply(store, e);
	};
	// Ordered by time, and by the order they were set when the times are equal. So a timer set for the
	// instant it is set at runs after everything already due then: a claim or a pull follows every ack and
	// notify that land with it.
	const timer = (at: number, fire: (now: number) => void) => {
		timers.push({ at, seq: seq++, fire });
		timers.sort((a, b) => a.at - b.at || a.seq - b.seq);
	};

	/** How many more jobs the worker may take: its limit less the rings that hold a job. */
	const credit = (w: Worker) => w.limit - rings(w).busy.size;

	// A ring takes a job: its record goes in flight on the lowest free slot, and a copy flies to the ring that went
	// idle last, or to a new goroutine on the lowest free ring. The ack frees the slot and puts the goroutine back in
	// its pool, where it stays or exits, and only then does the worker look again (`freed`).
	const take = (now: number, w: Worker, job: string, work: number, freed: (now: number) => void) => {
		const { busy, idle } = rings(w);
		if (idle.length === 0) emit({ at: now, type: 'spawn', consumer: w.id, ring: lowestFree(busy.keys()) });
		const ring = idle[idle.length - 1];
		const deaths = w.deaths;
		emit({ at: now, type: 'take', job, consumer: w.id, ring, tray: store.tray.indexOf(null) });
		timer(now + plan.takeTravel + work, (done) => {
			if (w.deaths !== deaths) return;
			emit({ at: done, type: 'done', job, consumer: w.id, ring });
			timer(done + plan.ackTravel, (ack) => {
				emit({ at: ack, type: 'ack', job, tray: store.tray.indexOf(job) });
				// `freePoolNode`: it stays while more jobs wait than the other goroutines idle, or while none of them is.
				const others = rings(w).idle.length - 1;
				if (store.queue.length <= others && others > 0) emit({ at: ack, type: 'exit', consumer: w.id, ring });
				freed(ack);
			});
		});
	};

	const run = () => {
		for (let t = timers.shift(); t; t = timers.shift()) t.fire(t.at);
		return events;
	};

	return { store, events, emit, timer, credit, take, run };
}

/** The crash part of a plan: all four fields, or none of them and no crash. */
function crashOf({ crashAt, restartAt, requeueAfter, rework }: SqlitePlan) {
	const given = [crashAt, restartAt, requeueAfter, rework].filter((x) => x !== undefined).length;
	if (given === 0) return undefined;
	if (crashAt === undefined || restartAt === undefined || requeueAfter === undefined || rework === undefined) {
		const names = 'crashAt, restartAt, requeueAfter and rework';
		throw new Error(`plan: ${names} go together, and only ${given} of them are given`);
	}
	return { crashAt, restartAt, requeueAfter, rework };
}

/**
 * One program with a worker and a jobs.db: Add() lands each job and the worker claims. With a crash in the
 * plan, the program dies with its jobs running, their records stay in flight, and the restart puts them back
 * at the head of the queue in write order; without one the run is plain add, claim, ack. The worker is the
 * consumer 'sqlite'. It refuses what the story cannot show: more rings than the tray has slots, a crash
 * while an ack is on its way, a job that lands while the program is down, and a crash given only in part.
 */
export function simulateSqlite(plan: SqlitePlan): StoreEvent[] {
	if (plan.limit > SLOTS) throw tooManyRings(plan.limit);
	const crash = crashOf(plan);

	const { store, events, emit, timer, credit, take, run } = engine(plan);
	const work = new Map(plan.jobs.map((j) => [j.id, j.work]));
	const row = new Map<string, number>(); // a job's row id in the file: its place in write order
	const program: Worker = { id: 'sqlite', limit: plan.limit, deaths: 0 };
	const rings = store.rings!.get(program.id)!;
	let open = true; // the program has the queue open: not from the crash to the requeue
	let looking = false; // the worker has started: from the first claim on, a land wakes it, as an Add() does
	let restarted = false;

	// A job takes its `work`; after the restart, its `rework`, or its `work` again when it has none.
	const duration = (job: string) => (restarted ? crash?.rework[job] : undefined) ?? work.get(job)!;

	// One statement: a job for every credit, from the head of the queue. Goroutines start as the claim needs them.
	const claim = (now: number) => {
		if (!open) return;
		for (const job of store.queue.slice(0, credit(program))) take(now, program, job, duration(job), wake);
	};
	const wake = (now: number) => timer(now, claim);

	if (crash) {
		timer(crash.crashAt, (now) => {
			// A job whose function has returned and whose ack is still on its way: the story cannot say if it landed.
			const acking = store.tray.find((job) => job && events.some((e) => e.type === 'done' && e.job === job));
			if (acking) {
				throw new Error(`crash: the ack of ${acking} is on its way at ${now} ms, unclear if it landed`);
			}
			program.deaths++;
			open = false;
			emit({ at: now, type: 'crash' });
			// Its goroutines die with it, busy and idle.
			for (const ring of [...rings.busy.keys(), ...rings.idle].sort((a, b) => a - b)) {
				emit({ at: now, type: 'exit', consumer: program.id, ring });
			}
		});
		timer(crash.restartAt, (now) => {
			emit({ at: now, type: 'restart' });
			emit({ at: now, type: 'spawn', consumer: program.id, ring: 0 }); // `Start()`
			timer(now + crash.requeueAfter, (at) => {
				// Every record in flight goes back to the head of the queue, in write order (the order of the row ids).
				const jobs = store.tray.filter((job): job is string => job !== null);
				jobs.sort((a, b) => row.get(a)! - row.get(b)!);
				emit({ at, type: 'requeue', jobs });
				open = true;
				restarted = true;
				wake(at);
			});
		});
	}
	for (const j of plan.jobs) {
		timer(j.at, (now) => {
			if (!open) throw new Error(`land: ${j.id} lands at ${now} ms, while the program is down`);
			row.set(j.id, row.size);
			emit({ at: now, type: 'land', job: j.id });
			if (looking) wake(now);
		});
	}
	timer(plan.firstClaim, (now) => {
		looking = true;
		wake(now);
	});

	return run();
}

/**
 * Program A adds, Redis holds the one queue, and each consumer program has a worker. A land notifies
 * every consumer. A consumer that is woken, by a notify or by the ack of one of its jobs, pulls one job
 * per credit, and the consumers woken together take in turn. It refuses more rings than the tray has
 * slots.
 */
export function simulateRedis(plan: RedisPlan): StoreEvent[] {
	const rings = plan.consumers.reduce((n, c) => n + c.limit, 0);
	if (rings > SLOTS) throw tooManyRings(rings);

	const { store, emit, timer, credit, take, run } = engine(plan);
	const work = new Map(plan.jobs.map((j) => [j.id, j.work]));
	const crew: Worker[] = plan.consumers.map((c) => ({ id: c.id, limit: c.limit, deaths: 0 }));
	const woken = new Set<Worker>(); // the consumers with something to act on since the last pull
	let turn = 0; // the consumer whose turn comes next

	// The woken consumers take the head of the queue in turn, one job each, and skip one with no credit.
	const deal = (now: number) => {
		for (;;) {
			const order = [...crew.slice(turn), ...crew.slice(0, turn)];
			const w = order.find((c) => woken.has(c) && credit(c) > 0);
			if (!w || store.queue.length === 0) break;
			const job = store.queue[0];
			take(now, w, job, work.get(job)!, (at) => wake(at, w));
			turn = (crew.indexOf(w) + 1) % crew.length;
		}
		woken.clear();
	};
	const wake = (now: number, w: Worker) => {
		woken.add(w);
		timer(now, deal);
	};

	for (const j of plan.jobs) {
		timer(j.at + plan.landAfter, (now) => {
			emit({ at: now, type: 'land', job: j.id });
			for (const w of crew) {
				timer(now + plan.notifyTravel, (at) => {
					emit({ at, type: 'notify', consumer: w.id });
					wake(at, w);
				});
			}
		});
	}

	return run();
}

/** Each worker's rings after each event, one state per event: the busy ones ascending, and the idle stack. */
export function ringStates(
	plan: SqlitePlan | RedisPlan,
	events: StoreEvent[],
): { at: number; rings: Record<string, { busy: number[]; idle: number[] }> }[] {
	const s = fresh(plan);

	return events.map((e) => {
		apply(s, e);
		const rings = [...s.rings!].map(([id, r]) => [id, { busy: [...r.busy.keys()].sort((a, b) => a - b), idle: [...r.idle] }]);
		return { at: e.at, rings: Object.fromEntries(rings) };
	});
}

/** Each worker's idle rings once the events have played: what its next chapter starts with. */
export function storeEnd(plan: SqlitePlan | RedisPlan, events: StoreEvent[]): Record<string, number[]> {
	const s = fresh(plan);
	for (const e of events) apply(s, e);

	return Object.fromEntries([...s.rings!].map(([id, r]) => [id, [...r.idle]]));
}

/** The tray after each event, one state per event: five slots, each the job in flight there or null. */
export function trayStates(events: StoreEvent[]): { at: number; tray: (string | null)[] }[] {
	const s = fresh();

	return events.map((e) => {
		apply(s, e);
		return { at: e.at, tray: [...s.tray] };
	});
}

/** The queue after each event, one state per event, the head first. */
export function queueStates(events: StoreEvent[]): { at: number; order: string[] }[] {
	const s = fresh();

	return events.map((e) => {
		apply(s, e);
		return { at: e.at, order: [...s.queue] };
	});
}
