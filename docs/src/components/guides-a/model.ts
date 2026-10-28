// The rules of the library that the guide scenes draw. Each function follows the Go source named
// above it, so a scene never shows a result that varmq cannot produce. model.test.ts checks the
// outputs that the guide pages print.

// ---- source_manager.go and internal/helpers/manager.go: which queue gives the next job ----

export type StrategyName = 'Priority' | 'RoundRobin' | 'MaxLen' | 'MinLen';

export interface Source {
	name: string;
	len: number;
}

export interface Pick {
	index: number;
	why: string;
}

/**
 * Picks the source for the next job. `sources` is the worker's list: ordered by queue priority,
 * and by bind order among equal numbers. `rr` is the round-robin index and moves like
 * Manager.roundRobinIndex. It returns null when every source is empty.
 */
export function pick(strategy: StrategyName, sources: Source[], rr: { i: number }): Pick | null {
	const n = sources.length;
	if (n === 0 || sources.every((s) => s.len === 0)) return null;

	switch (strategy) {
		case 'Priority': {
			const index = sources.findIndex((s) => s.len > 0);
			return { index, why: `${sources[index].name} is the first queue in the list that has a job.` };
		}
		case 'RoundRobin': {
			const skipped: string[] = [];
			for (;;) {
				const index = rr.i;
				rr.i = (rr.i + 1) % n;
				if (sources[index].len > 0) {
					const lead = skipped.length ? `${skipped.join(' and ')} ${skipped.length > 1 ? 'are' : 'is'} empty: skipped. ` : '';
					return { index, why: `${lead}It is the turn of ${sources[index].name}.` };
				}
				skipped.push(sources[index].name);
			}
		}
		case 'MaxLen': {
			const max = Math.max(...sources.map((s) => s.len));
			const tied = sources.map((s, i) => (s.len === max ? i : -1)).filter((i) => i >= 0);
			const index = tied[0];
			const why =
				tied.length > 1
					? `${tied.map((i) => sources[i].name).join(' and ')} tie at ${max}. ${sources[index].name} comes first in the list.`
					: `${sources[index].name} has the most waiting jobs: ${max}.`;
			return { index, why };
		}
		case 'MinLen': {
			const live = sources.filter((s) => s.len > 0);
			const min = Math.min(...live.map((s) => s.len));
			const tied = sources.map((s, i) => (s.len === min ? i : -1)).filter((i) => i >= 0);
			const index = tied[0];
			const why =
				tied.length > 1
					? `${tied.map((i) => sources[i].name).join(' and ')} tie at ${min}. ${sources[index].name} comes first in the list.`
					: `${sources[index].name} has the fewest waiting jobs: ${min}. Empty queues do not count.`;
			return { index, why };
		}
	}
}

// ---- internal/queues/heap.go: the in-memory priority queue ----

export interface Ranked {
	priority: number;
	seq: number;
}

/** The job that leaves a priority queue first: the lowest number, then the one added first. */
export function nextRanked<T extends Ranked>(items: T[]): T | undefined {
	let best: T | undefined;
	for (const it of items) {
		if (!best || it.priority < best.priority || (it.priority === best.priority && it.seq < best.seq)) best = it;
	}
	return best;
}

// ---- internal/queues/queue.go, config.go and group_job.go: job IDs and the in-queue check ----

export interface IdJob {
	key: number;
	label: string;
	/** What `ID()` returns. An empty string means the job has no ID. */
	id: string;
	/** True for an ID from WithJobId or Item.ID. Only a given ID is checked. */
	given: boolean;
	via: 'Add' | 'AddAll';
}

export interface IdResult {
	ok: boolean;
	job?: IdJob;
	id: string;
	/** The generator ran and its value was thrown away, because the caller gave an ID. */
	discarded?: string;
}

export class IdQueue {
	queue: IdJob[] = [];
	held = new Set<string>();
	generator = false;
	counter = 0;
	private seq = 0;

	/** The label the next stored job gets. */
	get nextLabel(): string {
		return `job ${this.seq + 1}`;
	}

	private generate(): string {
		return this.generator ? `auto-${++this.counter}` : '';
	}

	/** `choice` is the ID the caller gives, or '' for none. */
	add(via: 'Add' | 'AddAll', choice: string): IdResult {
		let id: string;
		let discarded: string | undefined;
		if (via === 'Add') {
			// Add always calls the generator, also when WithJobId follows.
			const gen = this.generate();
			if (choice) discarded = gen || undefined;
			id = choice || gen;
		} else {
			// AddAll calls the generator only for an item without an ID, then adds the g: prefix.
			const gen = choice ? '' : this.generate();
			id = choice ? `g:${choice}` : gen ? `g:${gen}` : '';
		}
		const given = choice !== '';
		if (given && this.held.has(id)) return { ok: false, id };
		const job: IdJob = { key: ++this.seq, label: `job ${this.seq}`, id, given, via };
		this.queue.push(job);
		if (given) this.held.add(id);
		return { ok: true, job, id, discarded };
	}

	/** A worker takes the first job. The queue frees its ID. */
	take(): IdJob | undefined {
		const job = this.queue.shift();
		if (job?.given) this.held.delete(job.id);
		return job;
	}

	/** Purge removes every job and frees every ID. */
	purge(): IdJob[] {
		const gone = this.queue;
		this.queue = [];
		this.held.clear();
		return gone;
	}
}

// ---- topic.go and internal/topics: one log, one cursor for each subscription ----

export interface TopicSub {
	name: string;
	subscribed: boolean;
	paused: boolean;
	/** The first payload this subscription receives. */
	from: number;
	read: Set<number>;
	/** Payloads that the worker took and made a job of. */
	jobs: number[];
}

export type PublishResult = { ok: true; n: number } | { ok: false; why: 'no subscribers' | 'full' };

export class Topic {
	next = 1;
	capacity = 0;
	log: number[] = [];
	subs: TopicSub[];

	constructor(names: string[]) {
		this.subs = names.map((name) => ({ name, subscribed: true, paused: false, from: 1, read: new Set(), jobs: [] }));
	}

	unread(s: TopicSub): number[] {
		return s.subscribed ? this.log.filter((n) => n >= s.from && !s.read.has(n)) : [];
	}

	publish(): PublishResult {
		const live = this.subs.filter((s) => s.subscribed);
		if (live.length === 0) return { ok: false, why: 'no subscribers' };
		// The limit applies to the slowest subscription.
		if (this.capacity > 0 && Math.max(...live.map((s) => this.unread(s).length)) >= this.capacity) {
			return { ok: false, why: 'full' };
		}
		const n = this.next++;
		this.log.push(n);
		return { ok: true, n };
	}

	/** A running worker reads every unread payload and makes its own job of each. A paused one reads nothing. */
	drain(s: TopicSub): number[] {
		if (!s.subscribed || s.paused) return [];
		const taken = this.unread(s);
		for (const n of taken) {
			s.read.add(n);
			s.jobs.push(n);
		}
		this.free();
		return taken;
	}

	subscribe(s: TopicSub): void {
		s.subscribed = true;
		s.from = this.next; // a subscription gets only the payloads published after it subscribed
		s.read = new Set();
	}

	/** The subscription drops what it has not read. A job it already took still runs. */
	unsubscribe(s: TopicSub): void {
		s.subscribed = false;
		this.free();
	}

	/** A payload leaves the log once every subscription has read it. */
	free(): void {
		const live = this.subs.filter((s) => s.subscribed);
		this.log = this.log.filter((n) => live.some((s) => n >= s.from && !s.read.has(n)));
	}
}
