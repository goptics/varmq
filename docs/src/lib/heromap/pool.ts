// One varmq worker as a discrete-event simulation: the event loop that hands jobs out and the pool of
// goroutines that runs them (`worker_eventloop.go`, `worker_pool.go`). Plans in, events out, for the chapter
// scripts to play back; nothing here knows about drawing. The jobs stand for slow ones: a goroutine below
// the limit is always worth starting (`worthSpawning`) and, above a limit of 1, a hand-off is one job
// (`batchBudget`). There is no idle expiry, and the pool keeps one goroutine idle (`numMinIdleWorkers`).
// A job is done `work` ms after it is dispatched: the animation flies it to its ring first and runs the
// ring's progress from arrival to done.

export interface SourceSpec {
	id: string;
	priority?: boolean; // priority: lowest number first, ties by arrival
}

export interface JobSpec {
	id: string;
	source: string;
	at: number;
	work: number;
	priority?: number;
}

export type ControlSpec =
	| { at: number; op: 'pause' | 'resume' | 'stop' }
	| { at: number; op: 'tune'; to: number };

export interface PoolPlan {
	limit: number;
	idle: number[]; // goroutine rings idle at t = 0, in the order they went idle: the last gets the next job
	sources: SourceSpec[]; // in bind order
	strategy?: 'roundrobin'; // needed with several sources: varmq defaults to Priority, which is not modelled
	jobs: JobSpec[];
	controls?: ControlSpec[];
	wake: number; // ms from a land, resume or tune to the dispatch wake-up
	gap: number; // ms between dispatches within one wake-up
	after: number; // ms from done to the freed goroutine's own wake-up
}

export type PoolEvent =
	| { at: number; type: 'land'; job: string; source: string }
	| {
			at: number;
			type: 'dispatch';
			job: string;
			source: string;
			ring: number;
			spawned: boolean;
			waiting: number; // jobs still queued once this one has been taken
	  }
	| { at: number; type: 'done'; job: string; ring: number }
	| { at: number; type: 'idle'; ring: number }
	| { at: number; type: 'exit'; ring: number }
	| { at: number; type: 'control'; op: 'pause' | 'resume' | 'stop' | 'tune'; to?: number };

/**
 * The pool after an event. `busy` ascends. `idle` is the pool's stack, so the ring that went idle last is at
 * the end and takes the next job. `waiting` lists each source's jobs head first. A ring whose job is done is
 * in neither list until the `idle` or `exit` that follows it at the same instant.
 */
export interface PoolState {
	at: number;
	busy: number[];
	idle: number[];
	waiting: Record<string, string[]>;
}

interface Pool {
	limit: number;
	busy: number[];
	idle: number[];
	waiting: Record<string, string[]>;
}

const lowestFree = (taken: number[]): number => {
	let ring = 0;
	while (taken.includes(ring)) ring++;
	return ring;
};

/**
 * The pool as the plan leaves it at t = 0, and `apply`, the one place an event changes it. `simulatePool`
 * runs every event it emits through `apply`, and `poolStates` and `endState` replay events through it, so
 * what the simulation decides on and what the replays report cannot drift apart.
 */
function track(plan: PoolPlan) {
	const jobs = new Map<string, JobSpec>(plan.jobs.map((j) => [j.id, j]));
	const byPriority = new Set(plan.sources.filter((s) => s.priority).map((s) => s.id));
	const rank = (id: string) => jobs.get(id)?.priority ?? 0;
	const pool: Pool = {
		limit: plan.limit,
		busy: [],
		idle: [...plan.idle],
		waiting: Object.fromEntries(plan.sources.map((s) => [s.id, []])),
	};

	function apply(e: PoolEvent): void {
		switch (e.type) {
			case 'land': {
				const queue = pool.waiting[e.source];
				// A priority source keeps its lowest number at the head; a tie goes behind the earlier arrival.
				let i = queue.length;
				if (byPriority.has(e.source)) while (i > 0 && rank(queue[i - 1]) > rank(e.job)) i--;
				queue.splice(i, 0, e.job);
				break;
			}
			case 'dispatch':
				pool.waiting[e.source] = pool.waiting[e.source].filter((id) => id !== e.job);
				pool.idle = pool.idle.filter((r) => r !== e.ring);
				pool.busy = [...pool.busy, e.ring].sort((a, b) => a - b);
				break;
			case 'done':
				pool.busy = pool.busy.filter((r) => r !== e.ring);
				break;
			case 'idle':
				pool.idle.push(e.ring);
				break;
			case 'exit':
				pool.idle = pool.idle.filter((r) => r !== e.ring);
				break;
			case 'control':
				if (e.op === 'tune' && e.to !== undefined) pool.limit = e.to;
				break;
		}
	}

	return { jobs, pool, apply };
}

export function simulatePool(plan: PoolPlan): PoolEvent[] {
	// Several sources are read by the worker's strategy, and the default one, Priority, is not modelled.
	if (plan.sources.length > 1 && plan.strategy !== 'roundrobin') {
		throw new Error(
			`strategy: several sources need 'roundrobin', as varmq's default, Priority, is not modelled`,
		);
	}

	const { jobs, pool, apply } = track(plan);
	const events: PoolEvent[] = [];
	const timers: { at: number; seq: number; fire: (now: number) => void }[] = [];
	let seq = 0;
	let paused = false;
	let stopped = false;
	let stepping = false; // a dispatch run is under way, with its next hand-out already timed
	let turn = 0; // round robin: the index of the source after the one served last

	const emit = (e: PoolEvent) => {
		events.push(e);
		apply(e);
	};
	// Ordered by time, and by the order they were set when the times are equal.
	const timer = (at: number, fire: (now: number) => void) => {
		timers.push({ at, seq: seq++, fire });
		timers.sort((a, b) => a.at - b.at || a.seq - b.seq);
	};
	const pending = () => Object.values(pool.waiting).reduce((n, queue) => n + queue.length, 0);
	// What the event loop's `for` asks: active, a slot below the limit (`busyWorkers < concurrency`), a job.
	const ready = () => !paused && !stopped && pool.busy.length < pool.limit && pending() > 0;

	// Hands the next source's head to the goroutine that went idle last, or to a new one on the lowest ring.
	const dispatch = (now: number) => {
		// One source is read as it is. Several are read round robin in bind order, skipping the empty ones,
		// and the turn moves past the one served (`GetRoundRobinItem`).
		const from = [...plan.sources.slice(turn), ...plan.sources.slice(0, turn)];
		const source = from.find((s) => pool.waiting[s.id].length > 0)!;
		turn = (plan.sources.indexOf(source) + 1) % plan.sources.length;
		const [job] = pool.waiting[source.id];
		const waiting = pending() - 1;
		// varmq hands a lone worker every waiting job at once. Only a single plain queue or a topic does, but
		// this refuses a priority or multi-source worker at limit 1 as well: the map has no batch to draw.
		if (pool.limit === 1 && waiting > 0) {
			throw new Error(
				`batch: ${waiting + 1} jobs wait at ${now} ms, and a worker at limit 1 takes them all at once`,
			);
		}
		const spawned = pool.idle.length === 0;
		const ring = spawned ? lowestFree(pool.busy) : pool.idle[pool.idle.length - 1];
		emit({ at: now, type: 'dispatch', job, source: source.id, ring, spawned, waiting });
		timer(now + jobs.get(job)!.work, (at) => finish(at, job, ring));
	};

	// A stop cancels the event loop, which runs `Wait` (until no job runs) and then `removeAllWorkers`: every
	// goroutine still in the pool is stopped, the last to go idle first. They leave together, at the last job.
	const retire = (now: number, rings: number[]) => {
		for (const ring of rings) emit({ at: now, type: 'exit', ring });
	};

	// `freePoolNode`: the goroutine stays idle while more jobs wait than goroutines sit idle, or while none
	// does, and otherwise exits at its own done. That holds while a stop is draining the running jobs too; the
	// one that finishes the last of them goes back in last, so `removeAllWorkers` stops it first. Then, as
	// after every job, it wakes the event loop.
	const finish = (now: number, job: string, ring: number) => {
		emit({ at: now, type: 'done', job, ring });
		if (stopped && pool.busy.length === 0) {
			retire(now, [ring, ...[...pool.idle].reverse()]);
		} else {
			const stays = pending() > pool.idle.length || pool.idle.length === 0;
			emit({ at: now, type: stays ? 'idle' : 'exit', ring });
		}
		timer(now + plan.after, wake);
	};

	// One turn of the event loop's `for`: hand out a job, and take the next one `gap` later while it still can.
	const step = (now: number) => {
		stepping = false;
		if (!ready()) return;
		dispatch(now);
		if (ready()) {
			stepping = true;
			timer(now + plan.gap, step);
		}
	};

	// The signal channel holds one wake-up. One that comes due while a run is still handing out jobs is
	// dropped: the run looks at the queue and the pool again at its next hand-out.
	const wake = (now: number) => {
		if (!stepping) step(now);
	};

	const control = (c: ControlSpec, now: number) => {
		// varmq answers these calls with an error (ErrWorkerPausing, ErrWorkerPaused, ErrWorkerStopped), and a
		// story must not show what it refuses. Lowering the limit is not modelled: TunePool would also stop
		// idle goroutines.
		if (c.op === 'resume' && paused && pool.busy.length > 0) {
			throw new Error(
				`resume: ${pool.busy.length} job(s) still run at ${now} ms, and a pausing worker cannot resume`,
			);
		}
		if (c.op === 'tune' && (paused || stopped)) {
			throw new Error(
				`tune: the worker is paused or stopped at ${now} ms, and only an active one can be tuned`,
			);
		}
		if (c.op === 'tune' && c.to <= pool.limit) {
			throw new Error(
				`tune: ${c.to} does not raise the limit of ${pool.limit}, and only raising it is modelled`,
			);
		}

		emit(
			c.op === 'tune'
				? { at: now, type: 'control', op: 'tune', to: c.to }
				: { at: now, type: 'control', op: c.op },
		);
		if (c.op === 'pause') paused = true;
		if (c.op === 'resume') paused = false;
		if (c.op === 'resume' || c.op === 'tune') timer(now + plan.wake, wake);
		if (c.op === 'stop') {
			stopped = true;
			// With no job running, `Wait` returns at once and the idle goroutines go now. Otherwise they wait
			// for the last running job to finish (see `finish`).
			if (pool.busy.length === 0) retire(now, [...pool.idle].reverse());
		}
	};

	// Controls are timed before jobs, so a control and a land at the same instant happen in that order.
	for (const c of plan.controls ?? []) timer(c.at, (now) => control(c, now));
	for (const j of plan.jobs) {
		timer(j.at, (now) => {
			emit({ at: now, type: 'land', job: j.id, source: j.source });
			timer(now + plan.wake, wake);
		});
	}
	for (let t = timers.shift(); t; t = timers.shift()) t.fire(t.at);

	return events;
}

export function poolStates(plan: PoolPlan, events: PoolEvent[]): PoolState[] {
	const { pool, apply } = track(plan);

	return events.map((e) => {
		apply(e);
		return {
			at: e.at,
			busy: [...pool.busy],
			idle: [...pool.idle],
			waiting: Object.fromEntries(Object.entries(pool.waiting).map(([id, queue]) => [id, [...queue]])),
		};
	});
}

export function endState(plan: PoolPlan, events: PoolEvent[]): { limit: number; idle: number[] } {
	const { pool, apply } = track(plan);
	for (const e of events) apply(e);

	return { limit: pool.limit, idle: [...pool.idle] };
}
