// The jobs JOB 02's scenes add, and the order they run in, worked out the way varmq works it out, so the scenes
// (scenes.ts) show what the library does and order.test.ts pins it. No DOM here: the page's still frames are drawn at build time too.

/** A job added to a priority queue: its name and its number. */
export interface Add {
	name: string;
	priority: number;
}

/** What examples/queues-priority adds, in order. */
export const PRIORITY_ADDS: Add[] = [
	{ name: 'report', priority: 10 },
	{ name: 'alert', priority: 1 },
	{ name: 'digest', priority: 10 },
];

/** The queues of examples/strategies-round-robin, in the order they are bound, and the jobs added to each. */
export const RR_QUEUES = [
	{ name: 'alerts', jobs: ['a1', 'a2'] },
	{ name: 'reports', jobs: ['r1'] },
	{ name: 'emails', jobs: ['e1', 'e2', 'e3'] },
];

/**
 * A priority queue's contents after each Add, head first. The lowest number is the head, and equal numbers keep the
 * order they were added in, as varmq's priority queue does (internal/queues: ties go by insertion index).
 */
export function priorityQueue(adds: Add[]): Add[][] {
	return adds.map((_, i) =>
		adds
			.slice(0, i + 1)
			.map((add, at) => ({ add, at }))
			.sort((a, b) => a.add.priority - b.add.priority || a.at - b.at)
			.map(({ add }) => add),
	);
}

/** One job the RoundRobin strategy takes: the queue it comes from, the job, and the empty queues passed over first. */
export interface Pick {
	queue: number;
	job: string;
	skipped: number[];
}

/**
 * The jobs a worker with these queues takes, in order, under RoundRobin, with every job added before the worker
 * starts (WithAutoRun(false)) and nothing added after. Each queue is its jobs, head first, in the order the queues
 * were bound. This is helpers.Manager.GetRoundRobinItem: from the queue after the last one taken, the first that has a
 * job, passing over the empty ones; once every queue is empty there is nothing to take.
 */
export function roundRobin(queues: string[][]): Pick[] {
	const left = queues.map((q) => [...q]);
	const picks: Pick[] = [];
	let index = 0;
	for (;;) {
		const start = index;
		const skipped: number[] = [];
		let found = -1;
		do {
			const q = index;
			index = (index + 1) % left.length;
			if (left[q].length > 0) {
				found = q;
				break;
			}
			skipped.push(q);
		} while (index !== start);
		if (found < 0) return picks;
		picks.push({ queue: found, job: left[found].shift()!, skipped });
	}
}
