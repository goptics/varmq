// A small model of the worker pool. The rules are the ones in worker_eventloop.go and worker_pool.go:
// worthSpawning, batchSize, batchBudget, planBatch and freePoolNode. Time is virtual and in milliseconds.

export const MAX_BATCH = 32;
const SPAWN_NANOS = 2_000;
const HANDOFF_GAP = 32_000;

export function worthSpawning(pending: number, jobNanos: number, alive: number) {
	if (alive === 0 || jobNanos === 0) return true; // nothing measured yet: fair share decides
	return pending * jobNanos > SPAWN_NANOS * alive * alive;
}

export function batchSize(pending: number, shares: number) {
	return Math.max(1, Math.min(Math.floor(pending / Math.max(shares, 1)), MAX_BATCH));
}

export function batchBudget(jobNanos: number, alive: number) {
	if (jobNanos === 0) return 1;
	const need = Math.ceil((Math.max(alive, 1) * HANDOFF_GAP) / jobNanos);
	return Math.max(1, Math.min(need, MAX_BATCH));
}

export interface Plan {
	node: number;
	isNew: boolean;
	k: number;
	alive: number;
	shares: number;
	even: number;
	budget: number;
}

/** What planBatch decides for the next hand-off. `null` means: no idle goroutine, and a new one is not worth it. */
export function plan(opts: {
	pending: number;
	concurrency: number;
	jobNanos: number;
	alive: number;
	idleNode: number | null;
	newNode: number;
}): Plan | null {
	const { pending, concurrency, jobNanos, alive } = opts;
	let shares = Math.min(concurrency, pending);
	const grow = worthSpawning(pending, jobNanos, alive);
	if (!grow) shares = alive;

	let node = opts.idleNode;
	let isNew = false;
	if (node === null) {
		if (!grow) return null;
		node = opts.newNode;
		isNew = true;
	}

	const budget = concurrency > 1 ? batchBudget(jobNanos, alive) : MAX_BATCH;
	const even = batchSize(pending, shares);
	return { node, isNew, k: Math.min(even, budget, MAX_BATCH), alive, shares, even, budget };
}

export interface Batch {
	node: number;
	jobs: number[];
	start: number;
	end: number;
}

export type Step =
	| { kind: 'add'; n: number }
	| { kind: 'wake' }
	| { kind: 'handoff'; node: number; jobs: number[]; spawned: boolean }
	| { kind: 'sleep'; why: 'empty' | 'full' }
	| { kind: 'finish'; node: number; jobs: number[]; kept: boolean }
	| { kind: 'wait' }
	| { kind: 'idle' };

/**
 * One worker. `advance()` applies the next atomic step and returns it, so a scene can play the steps
 * with its own pacing and a reader can step through them by hand.
 */
export class PoolModel {
	concurrency: number;
	/** Average ns per job in the last finished batch. 0 until one has run. */
	jobNanos: number;
	jobMs: number;
	queue: number[] = [];
	idle: number[] = [1]; // the pool starts with one idle goroutine
	busy: Batch[] = [];
	done: number[] = [];
	now = 0;
	signal = false;
	dropped = 0;
	awake = false;
	private next = 1;
	private phase: 'rest' | 'dispatch' = 'rest';

	constructor(concurrency: number, jobMs: number, measured = false) {
		this.concurrency = concurrency;
		this.jobMs = jobMs;
		this.jobNanos = measured ? jobMs * 1e6 : 0;
	}

	get alive() {
		return this.busy.length + this.idle.length;
	}

	get minIdle() {
		return 1; // WithMinIdleWorkerRatio is 0 by default, and the floor is one
	}

	private lowestFreeNode() {
		const used = new Set([...this.idle, ...this.busy.map((b) => b.node)]);
		let n = 1;
		while (used.has(n)) n++;
		return n;
	}

	/** Add n jobs. Every Add tries the signal slot; the slot holds one. */
	add(n: number): Step {
		for (let i = 0; i < n; i++) this.queue.push(this.next++);
		for (let i = 0; i < n; i++) {
			if (this.signal) this.dropped++;
			else this.signal = true;
		}
		return { kind: 'add', n };
	}

	get over() {
		return this.queue.length === 0 && this.busy.length === 0 && !this.signal && this.phase === 'rest';
	}

	/** When the first running batch ends, or null when nothing runs. */
	nextDue(): number | null {
		return this.busy.length ? Math.min(...this.busy.map((b) => b.end)) : null;
	}

	/** True while the loop has work of its own to do: a signal to take or a dispatch to finish. */
	get loopBusy() {
		return this.signal || this.phase === 'dispatch';
	}

	private finishBatch(at: number): Step {
		const [b] = this.busy.splice(at, 1);
		this.done.push(...b.jobs);
		this.jobNanos = this.jobMs * 1e6;
		// freePoolNode: keep the goroutine while more jobs wait than idle ones can take,
		// or while the pool holds fewer than the minimum of idle goroutines.
		const kept = this.queue.length > this.idle.length || this.idle.length < this.minIdle;
		if (kept) this.idle.push(b.node);
		this.signal = true;
		return { kind: 'finish', node: b.node, jobs: b.jobs, kept };
	}

	advance(now = this.now): Step {
		this.now = Math.max(this.now, now);

		// A goroutine whose batch has ended comes back before the loop wakes.
		if (this.phase === 'rest') {
			let at = -1;
			for (let i = 0; i < this.busy.length; i++) if (this.busy[i].end <= this.now && (at < 0 || this.busy[i].end < this.busy[at].end)) at = i;
			if (at >= 0) return this.finishBatch(at);
		}

		if (this.signal && this.phase === 'rest') {
			this.signal = false;
			this.awake = true;
			this.phase = 'dispatch';
			return { kind: 'wake' };
		}

		if (this.phase === 'dispatch') {
			if (this.busy.length < this.concurrency && this.queue.length > 0) {
				const p = plan({
					pending: this.queue.length,
					concurrency: this.concurrency,
					jobNanos: this.jobNanos,
					alive: this.alive,
					idleNode: this.idle.length ? this.idle[this.idle.length - 1] : null,
					newNode: this.lowestFreeNode(),
				});
				if (p) {
					if (!p.isNew) this.idle.pop();
					const jobs = this.queue.splice(0, p.k);
					this.busy.push({ node: p.node, jobs, start: this.now, end: this.now + jobs.length * this.jobMs });
					return { kind: 'handoff', node: p.node, jobs, spawned: p.isNew };
				}
			}
			this.awake = false;
			this.phase = 'rest';
			return { kind: 'sleep', why: this.queue.length === 0 ? 'empty' : 'full' };
		}

		return this.busy.length ? { kind: 'wait' } : { kind: 'idle' };
	}
}
