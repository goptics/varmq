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
