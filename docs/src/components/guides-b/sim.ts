// A small model of one varmq worker, shared by the guide scenes.
//
// It follows the Go sources (worker.go, worker_eventloop.go, worker_pool.go, worker_lifecycle.go,
// worker_park.go, worker_wait.go, metrics.go). It models jobs that run for a visible time
// (hundreds of milliseconds and up), so two Go details are left out on purpose:
//   - worthSpawning: for slow jobs a goroutine is always worth starting, up to the limit.
//   - batchBudget: for slow jobs a batch is one job when the limit is above 1.
// Time is virtual, in milliseconds. The scenes call advance() from a clock they own.

export type Status = 'Initiated' | 'Idle' | 'Running' | 'Pausing' | 'Paused' | 'Stopping' | 'Stopped';
export type Where = 'queue' | 'batch' | 'parked' | 'lane' | 'out';
export type JobState = 'queued' | 'running' | 'done' | 'failed' | 'closed';
export type Fail = 'error' | 'panic';
export type CtxErr = 'canceled' | 'deadline';

export interface Job {
	id: number;
	dur: number;
	left: number;
	state: JobState;
	at: Where;
	fail?: Fail;
	/** The function selects on Context().Done() and returns early. */
	watch?: boolean;
	early?: boolean;
	stale?: boolean;
	slot?: number;
	added: number;
	started?: number;
	ended?: number;
}

export interface Gor {
	id: number;
	slot: number;
	busy: boolean;
	batch: Job[];
	i: number;
}

export type SimEvent =
	| { type: 'add'; job: Job }
	| { type: 'start'; job: Job }
	| { type: 'end'; job: Job }
	| { type: 'park'; jobs: Job[] }
	| { type: 'close'; jobs: Job[] }
	| { type: 'status'; from: Status; to: Status }
	| { type: 'spawn'; gor: Gor }
	| { type: 'exit'; gor: Gor };

export interface Metrics {
	submitted: number;
	completed: number;
	successful: number;
	failed: number;
}

/** maxBatch in worker_eventloop.go. */
const MAX_BATCH = 32;
const STEP = 40;

export class WorkerSim {
	now = 0;
	status: Status;
	limit: number;
	/** WithMinIdleWorkerRatio. 0 means not set. */
	minIdlePct = 0;
	/** WithIdleWorkerExpiryDuration in ms. 0 means no timer. */
	expiry = 0;
	queue: Job[] = [];
	parked: Job[] = [];
	gors: Gor[] = [];
	pool: Gor[] = [];
	curProcessing = 0;
	m: Metrics = { submitted: 0, completed: 0, successful: 0, failed: 0 };
	/** The context the caller passed with WithContext. */
	userCtx: CtxErr | null = null;
	/** The worker's own Context(). It is a child of the caller's context. */
	ctx: CtxErr | null = null;
	onEvent: (e: SimEvent) => void = () => {};

	private restartPending = false;
	private nextJob = 1;
	private nextGor = 1;
	private expiryClock = 0;
	private idleSince = new Map<number, number>();

	constructor(opts: { limit?: number; autoRun?: boolean } = {}) {
		this.limit = opts.limit ?? 1;
		this.status = 'Initiated';
		if (opts.autoRun !== false) this.start();
	}

	// ---- reads: the methods on Worker ----

	numConcurrency() {
		return this.limit;
	}
	numProcessing() {
		return this.busy();
	}
	numIdleWorkers() {
		return this.pool.length;
	}
	/** Jobs in the queue, parked, and taken into a batch but not running: worker.go NumPending. */
	numPending() {
		return this.queue.length + this.parked.length + Math.max(0, this.curProcessing - this.busy());
	}
	/** The queue's own Len(): only what is still in the queue. */
	queueLen() {
		return this.queue.length;
	}
	busy() {
		let n = 0;
		for (const g of this.gors) if (g.busy) n++;
		return n;
	}
	isActive() {
		return this.status === 'Idle' || this.status === 'Running';
	}
	halting() {
		return this.status === 'Pausing' || this.status === 'Paused' || this.status === 'Stopping' || this.status === 'Stopped';
	}
	minIdle() {
		return Math.max(Math.floor((this.limit * this.minIdlePct) / 100), 1);
	}
	/** Ms until the idle goroutine timer checks the pool again. 0 when there is no timer. */
	sweepIn() {
		return this.expiry > 0 ? Math.max(0, this.expiry - this.expiryClock) : 0;
	}
	jobs(): Job[] {
		const out: Job[] = [];
		for (const g of this.gors) for (const j of g.batch) if (j.at === 'lane' || j.at === 'batch') out.push(j);
		return [...this.queue, ...this.parked, ...out];
	}

	// ---- calls ----

	/** Queue.Add. It returns the job; an in-memory queue with no capacity always takes it. */
	add(o: { dur?: number; fail?: Fail; watch?: boolean } = {}): Job {
		const job: Job = {
			id: this.nextJob++,
			dur: o.dur ?? 2400,
			left: o.dur ?? 2400,
			state: 'queued',
			at: 'queue',
			fail: o.fail,
			watch: o.watch,
			added: this.now,
		};
		this.queue.push(job);
		this.m.submitted++;
		this.onEvent({ type: 'add', job });
		this.dispatch();
		return job;
	}

	start(): string | null {
		if (this.isActive()) return 'ErrRunningWorker';
		if (this.status !== 'Initiated' && this.status !== 'Stopped') return this.statusError();
		// A worker that starts derives its context from the caller's, which may be done already.
		this.ctx = this.userCtx;
		this.pool.push(this.spawn());
		this.setStatus('Idle');
		this.dispatch();
		return null;
	}

	stop(): string | null {
		if (this.status === 'Stopped' || this.status === 'Stopping') return null;
		if (this.status === 'Initiated') return 'ErrNotRunningWorker';
		this.ctx = this.userCtx ?? 'canceled';
		this.setStatus('Stopping');
		return null;
	}

	pause(): string | null {
		if (this.status === 'Paused' || this.status === 'Pausing') return null;
		if (this.status === 'Idle') {
			this.setStatus('Paused');
			return null;
		}
		if (this.status === 'Running') {
			this.setStatus('Pausing');
			if (this.curProcessing === 0) this.setStatus('Paused');
			return null;
		}
		return this.statusError();
	}

	resume(): string | null {
		if (this.status === 'Paused') {
			this.setStatus('Idle');
			this.dispatch();
			return null;
		}
		if (this.isActive()) return null;
		return this.statusError();
	}

	/** Stop, wait for Stopped, then Start with a fresh context, as Restart does. */
	restart(): string | null {
		if (this.status === 'Initiated') return 'ErrNotRunningWorker';
		if (this.status === 'Stopped') return this.start();
		this.stop();
		this.restartPending = true;
		return null;
	}

	tunePool(n: number): string | null {
		if (!this.isActive()) return this.statusError();
		if (this.limit === n) return 'ErrSameConcurrency';
		const old = this.limit;
		this.limit = n;
		if (n > old) {
			this.dispatch();
			return null;
		}
		if (this.expiry !== 0) return null;
		let shrink = old - n;
		const min = this.minIdle();
		while (shrink > 0 && this.pool.length !== min) {
			const g = this.pool.pop();
			if (!g) break;
			this.exit(g);
			shrink--;
		}
		return null;
	}

	/** Queue.Purge: close what waits in the queue and in the park; a batch is closed as the goroutine reaches it. */
	purge(): number {
		let n = 0;
		const closed: Job[] = [];
		for (const j of this.queue) {
			j.state = 'closed';
			closed.push(j);
		}
		n += this.queue.length;
		this.queue = [];
		for (const j of this.parked) {
			j.state = 'closed';
			closed.push(j);
		}
		n += this.parked.length;
		this.parked = [];
		for (const g of this.gors) {
			if (!g.busy) continue;
			for (let k = g.i + 1; k < g.batch.length; k++) {
				g.batch[k].stale = true;
				n++;
			}
		}
		if (closed.length) this.onEvent({ type: 'close', jobs: closed });
		return n;
	}

	resetMetrics() {
		this.m = { submitted: 0, completed: 0, successful: 0, failed: 0 };
	}

	/** The context the caller passed is done. The worker notices on its next turn. */
	cancelUser(why: CtxErr) {
		if (this.userCtx) return;
		this.userCtx = why;
		if (!this.ctx) this.ctx = why;
	}

	statusError(): string {
		switch (this.status) {
			case 'Paused':
				return 'ErrWorkerPaused';
			case 'Stopped':
				return 'ErrWorkerStopped';
			case 'Pausing':
				return 'ErrWorkerPausing';
			case 'Stopping':
				return 'ErrWorkerStopping';
			case 'Running':
			case 'Idle':
				return 'ErrRunningWorker';
			default:
				return 'ErrNotRunningWorker';
		}
	}

	// ---- waits: worker_wait.go ----

	waitDone(): boolean {
		switch (this.status) {
			case 'Running':
			case 'Idle':
				return this.numPending() === 0 && this.curProcessing === 0;
			case 'Paused':
			case 'Stopped':
			case 'Pausing':
			case 'Stopping':
				return this.curProcessing === 0;
			default:
				return true;
		}
	}
	waitUntilIdleDone(): boolean {
		return this.status === 'Idle' && this.numPending() === 0 && this.curProcessing === 0;
	}
	waitUntilPausedDone(): boolean {
		return this.status === 'Paused';
	}
	waitUntilStoppedDone(): boolean {
		return this.status === 'Stopped';
	}

	// ---- time ----

	/** True while the model still changes without a call: a job runs, jobs wait for a free goroutine, or a stop is under way. */
	moving(): boolean {
		return this.busy() > 0 || this.status === 'Stopping' || (this.isActive() && this.queue.length + this.parked.length > 0);
	}

	advance(ms: number) {
		let left = ms;
		while (left > 0) {
			const dt = Math.min(left, STEP);
			left -= dt;
			this.step(dt);
		}
	}

	private step(dt: number) {
		this.now += dt;

		// The event loop's <-ctx.Done() case.
		if (this.ctx && (this.isActive() || this.status === 'Pausing' || this.status === 'Paused')) {
			this.setStatus('Stopping');
		}

		for (const g of [...this.gors]) {
			if (!g.busy) continue;
			const j = g.batch[g.i];
			if (j.watch && this.ctx && !j.early) {
				j.early = true;
				j.left = 0;
			}
			j.left -= dt;
			if (j.left <= 0) this.finishJob(g, j);
		}

		// Stopping: Wait for the running jobs, remove every goroutine, then Stopped.
		if (this.status === 'Stopping' && this.curProcessing === 0) {
			for (const g of [...this.pool]) this.exit(g);
			this.pool = [];
			this.setStatus('Stopped');
			if (this.restartPending) {
				this.restartPending = false;
				this.start();
			}
		}

		if (this.expiry > 0 && this.isActive()) this.expire(dt);
		this.dispatch();
	}

	// ---- inside ----

	private setStatus(to: Status) {
		if (to === this.status) return;
		const from = this.status;
		this.status = to;
		this.onEvent({ type: 'status', from, to });
	}

	private spawn(): Gor {
		const used = new Set(this.gors.map((g) => g.slot));
		let slot = 0;
		while (used.has(slot)) slot++;
		const g: Gor = { id: this.nextGor++, slot, busy: false, batch: [], i: 0 };
		this.gors.push(g);
		this.idleSince.set(g.id, this.now);
		this.onEvent({ type: 'spawn', gor: g });
		return g;
	}

	private exit(g: Gor) {
		this.gors = this.gors.filter((x) => x !== g);
		this.pool = this.pool.filter((x) => x !== g);
		this.idleSince.delete(g.id);
		this.onEvent({ type: 'exit', gor: g });
	}

	/** processNextJob: hand a batch to one goroutine, while one is free and jobs wait. */
	private dispatch() {
		while (this.isActive() && this.busy() < this.limit && this.queue.length + this.parked.length > 0) {
			const pending = this.queue.length + this.parked.length;
			// planBatch: a lone goroutine takes the backlog in one batch, up to maxBatch; with more
			// than one, slow jobs make the batch budget 1.
			const k = this.limit === 1 ? Math.min(pending, MAX_BATCH) : 1;
			const g = this.pool.pop() ?? this.spawn();

			const buf: Job[] = [];
			while (buf.length < k && this.parked.length) buf.push(this.parked.shift()!);
			while (buf.length < k && this.queue.length) {
				const j = this.queue.shift()!;
				if (j.state === 'closed') continue;
				buf.push(j);
			}
			if (buf.length === 0) {
				this.pool.push(g);
				break;
			}

			g.busy = true;
			g.batch = buf;
			g.i = 0;
			this.curProcessing += buf.length;
			if (this.status === 'Idle') this.setStatus('Running');
			for (const j of buf) j.at = 'batch';
			this.begin(g, 0);
		}
	}

	private begin(g: Gor, i: number) {
		const j = g.batch[i];
		g.i = i;
		j.state = 'running';
		j.at = 'lane';
		j.slot = g.slot;
		j.started = this.now;
		this.onEvent({ type: 'start', job: j });
	}

	private finishJob(g: Gor, j: Job) {
		j.state = j.fail ? 'failed' : 'done';
		j.at = 'out';
		j.ended = this.now;
		if (j.fail) this.m.failed++;
		else this.m.successful++;
		this.m.completed++;
		this.onEvent({ type: 'end', job: j });

		let next = g.i + 1;
		while (next < g.batch.length) {
			const nj = g.batch[next];
			// A job purged after it was taken is closed, not run.
			if (nj.stale) {
				nj.state = 'closed';
				nj.at = 'out';
				this.onEvent({ type: 'close', jobs: [nj] });
				next++;
				continue;
			}
			// Stopping or pausing: park the unstarted rest. The first job of a batch always runs.
			if (this.halting()) {
				const rest = g.batch.slice(next).filter((x) => x.state !== 'closed');
				for (const r of rest) r.at = 'parked';
				this.parked.push(...rest);
				this.onEvent({ type: 'park', jobs: rest });
				break;
			}
			this.begin(g, next);
			return;
		}
		this.endBatch(g);
	}

	/** freePoolNode, then finishJobs and releaseWaiters. */
	private endBatch(g: Gor) {
		const k = g.batch.length;
		g.busy = false;
		g.batch = [];
		g.i = 0;
		this.idleSince.set(g.id, this.now);

		// Keep the goroutine while more jobs wait than idle goroutines can run, while a timer
		// removes idle ones, or while the pool is below its minimum.
		const pending = this.queue.length + this.parked.length;
		if (pending > this.pool.length || this.expiry > 0 || this.pool.length < this.minIdle()) this.pool.push(g);
		else this.exit(g);

		this.curProcessing -= k;
		if (this.curProcessing === 0) {
			if (this.status === 'Running') this.setStatus('Idle');
			else if (this.status === 'Pausing') this.setStatus('Paused');
		}
	}

	/** goRemoveIdleWorkers: once per duration, drop goroutines idle longer than the duration, down to the minimum. */
	private expire(dt: number) {
		this.expiryClock += dt;
		if (this.expiryClock < this.expiry) return;
		this.expiryClock = 0;
		const target = this.minIdle();
		if (this.pool.length <= target) return;
		const keep: Gor[] = [];
		let len = this.pool.length;
		for (const g of this.pool) {
			const idleFor = this.now - (this.idleSince.get(g.id) ?? this.now);
			if (len > target && idleFor > this.expiry) {
				this.exit(g);
				len--;
			} else keep.push(g);
		}
		this.pool = keep;
	}
}
