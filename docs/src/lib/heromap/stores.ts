// The SQLite and Redis stores as discrete-event simulations: a queue lane and an in-flight tray in the
// store, and the workers that take jobs from it. Plans in, events out, for the chapter scripts to play
// back; nothing here knows about drawing. The rules come from persistence/overview, sqlite and redis in
// these docs:
// - A taken job stays in the store as a record in flight (a `processing` row, an entry in `<key>:nacked`)
//   until its ack lands. varmq acks when the job's function returns, before the goroutine goes back to
//   the pool (`j.Close()` in `worker_pool.go`), so a ring takes its next job only after its ack, and holds
//   one record at a time.
// - sqliteq: a worker with this queue alone claims one job per free ring in one statement
//   (`dispatchCredit`). Rows come out by id. Opening the queue sets every `processing` row that was never
//   acked back to `pending` (`requeueNoAck`); claims take the lowest ids, so those rows sort ahead of
//   every row still waiting, in the order they were written.
// - redisq v1.4.0: an add pushes the job on a list and publishes a notification that every consumer gets.
//   A woken worker pulls one job per free ring, and a job one consumer has taken is not in the list for
//   another. Which consumer wins a job is a race on the Redis server; here the woken consumers take in
//   turn, one job each, as a stand-in for it. A job whose process died is requeued only once its
//   visibility timeout (five minutes by default) has passed, so there is no crash in the Redis story.
// A job runs `takeTravel` after its take (the copy's flight to the ring) and is done `work` ms later; the
// animation runs the ring's progress from arrival to done. Its ack lands `ackTravel` after that. The tray
// has five slots, and a record holds the lowest free one from its take until its ack, so a store's rings
// (five at most) bound it. Events at one instant come in the order they were set: SQLite sets the crash
// before the lands, so a land at the instant of the crash comes after it, and a claim or a pull is set by
// whatever wakes it, so it follows everything already due at that instant.

export interface StoreJob {
	id: string;
	// SQLite: when its row is written, which lands it. Redis: when program A adds it, landAfter before it lands.
	at: number;
	work: number;
}

export interface SqlitePlan {
	rings: number;
	jobs: StoreJob[];
	firstClaim: number; // when the worker first looks at the queue, after the burst has landed
	crashAt: number;
	restartAt: number;
	requeueAfter: number; // ms from the restart to the queue being opened: the requeue, and a claim at once
	// The work of a run after the restart. A job with no entry takes its `work` again.
	rework: Record<string, number>;
	takeTravel: number;
	ackTravel: number;
}

export interface RedisPlan {
	consumers: { id: string; rings: number }[]; // in notify order
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
	| { at: number; type: 'take'; job: string; consumer: string; ring: number; tray: number } // goes in flight
	| { at: number; type: 'done'; job: string; consumer: string; ring: number } // the function returns
	| { at: number; type: 'ack'; job: string; tray: number } // the record is gone and the ring is free
	| { at: number; type: 'crash' }
	| { at: number; type: 'restart' }
	| { at: number; type: 'requeue'; jobs: string[] }; // the records in flight, back at the queue head

const SLOTS = 5;

interface Store {
	queue: string[];
	tray: (string | null)[];
}

const fresh = (): Store => ({ queue: [], tray: Array<string | null>(SLOTS).fill(null) });

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
		case 'take': {
			const i = s.queue.indexOf(e.job);
			if (i < 0) throw new Error(`take: ${e.job} is not waiting at ${e.at} ms`);
			if (s.tray[e.tray] !== null) {
				throw new Error(`take: slot ${e.tray} is not free for ${e.job} at ${e.at} ms`);
			}
			s.queue.splice(i, 1);
			s.tray[e.tray] = e.job;
			break;
		}
		case 'ack':
			if (s.tray[e.tray] !== e.job) {
				throw new Error(`ack: slot ${e.tray} does not hold ${e.job} at ${e.at} ms`);
			}
			s.tray[e.tray] = null;
			break;
		case 'requeue':
			for (const job of e.jobs) {
				const i = s.tray.indexOf(job);
				if (i < 0) throw new Error(`requeue: ${job} is not in flight at ${e.at} ms`);
				s.tray[i] = null;
			}
			s.queue.unshift(...e.jobs);
			break;
		// notify, done, crash and restart change neither the queue nor the tray
	}
}

/** One program's worker. `rings` holds the job on each ring, from its take until its ack lands. */
interface Worker {
	id: string;
	rings: (string | null)[];
	deaths: number; // what a ring was going to do before its program last died never happens
}

const worker = (id: string, rings: number): Worker => ({
	id,
	rings: Array<string | null>(rings).fill(null),
	deaths: 0,
});

const tooManyRings = (rings: number) =>
	new Error(`rings: ${rings} rings can hold ${rings} records in flight, and the tray has ${SLOTS} slots`);

function engine(plan: { takeTravel: number; ackTravel: number }) {
	const store = fresh();
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

	// A ring takes a job: its record goes in flight on the lowest free slot, and a copy flies to the lowest
	// free ring. The ack frees the ring and the slot, and only then does the worker look again (`freed`).
	const take = (now: number, w: Worker, job: string, work: number, freed: (now: number) => void) => {
		const ring = w.rings.indexOf(null);
		const deaths = w.deaths;
		w.rings[ring] = job;
		emit({ at: now, type: 'take', job, consumer: w.id, ring, tray: store.tray.indexOf(null) });
		timer(now + plan.takeTravel + work, (done) => {
			if (w.deaths !== deaths) return;
			emit({ at: done, type: 'done', job, consumer: w.id, ring });
			timer(done + plan.ackTravel, (ack) => {
				emit({ at: ack, type: 'ack', job, tray: store.tray.indexOf(job) });
				w.rings[ring] = null;
				freed(ack);
			});
		});
	};

	const run = () => {
		for (let t = timers.shift(); t; t = timers.shift()) t.fire(t.at);
		return events;
	};

	return { store, events, emit, timer, take, run };
}

/**
 * One program with a worker and a jobs.db: Add() lands each job, the worker claims, a crash leaves its
 * running jobs in flight, and the restart puts them back at the head. The worker is the consumer
 * 'sqlite'. It refuses what the story cannot show: more rings than the tray has slots, a crash while an
 * ack is on its way, and a job that lands while the program is down.
 */
export function simulateSqlite(plan: SqlitePlan): StoreEvent[] {
	if (plan.rings > SLOTS) throw tooManyRings(plan.rings);

	const { store, events, emit, timer, take, run } = engine(plan);
	const work = new Map(plan.jobs.map((j) => [j.id, j.work]));
	const row = new Map<string, number>(); // a job's id in the file: the order its row was written
	const program = worker('sqlite', plan.rings);
	let open = true; // the program has the queue open: not from the crash to the requeue
	let looking = false; // the worker has started: from the first claim on, a land wakes it, as an Add() does
	let restarted = false;

	// A job takes its `work`; after the restart, its `rework`, or its `work` again when it has none.
	const duration = (job: string) => (restarted ? plan.rework[job] : undefined) ?? work.get(job)!;

	// One statement: a job for every free ring, from the head of the queue.
	const claim = (now: number) => {
		if (!open) return;
		const free = program.rings.filter((ring) => ring === null).length;
		for (const job of store.queue.slice(0, free)) take(now, program, job, duration(job), wake);
	};
	const wake = (now: number) => timer(now, claim);

	timer(plan.crashAt, (now) => {
		// A job whose function has returned and whose ack is still on its way: the story cannot say if it landed.
		const acking = store.tray.find((job) => job && events.some((e) => e.type === 'done' && e.job === job));
		if (acking) {
			throw new Error(`crash: the ack of ${acking} is on its way at ${now} ms, so it is unclear if it landed`);
		}
		program.deaths++;
		program.rings.fill(null);
		open = false;
		emit({ at: now, type: 'crash' });
	});
	timer(plan.restartAt, (now) => {
		emit({ at: now, type: 'restart' });
		timer(now + plan.requeueAfter, (at) => {
			// Every record in flight goes back to the head of the queue, in the order the rows were written.
			const jobs = store.tray.filter((job): job is string => job !== null);
			jobs.sort((a, b) => row.get(a)! - row.get(b)!);
			emit({ at, type: 'requeue', jobs });
			open = true;
			restarted = true;
			wake(at);
		});
	});
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
 * per free ring, and the consumers woken together take in turn. It refuses more rings than the tray has
 * slots.
 */
export function simulateRedis(plan: RedisPlan): StoreEvent[] {
	const rings = plan.consumers.reduce((n, c) => n + c.rings, 0);
	if (rings > SLOTS) throw tooManyRings(rings);

	const { store, emit, timer, take, run } = engine(plan);
	const work = new Map(plan.jobs.map((j) => [j.id, j.work]));
	const crew = plan.consumers.map((c) => worker(c.id, c.rings));
	const woken = new Set<Worker>(); // the consumers with something to act on since the last pull
	let turn = 0; // the consumer whose turn comes next

	// The woken consumers take the head of the queue in turn, one job each, and skip one with no free ring.
	const deal = (now: number) => {
		for (;;) {
			const order = [...crew.slice(turn), ...crew.slice(0, turn)];
			const w = order.find((c) => woken.has(c) && c.rings.includes(null));
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
