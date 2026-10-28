// The hero map's story: ten chapters, each a camera stop with its captions and the simulations that play
// there. Pure data for the renderer and the choreographer, which draw and animate exactly these events.
// Every land, dispatch, take and ack comes out of the pool and store simulations (`pool.ts`, `stores.ts`),
// so the map can only show what varmq does; the plans here choose when things happen and how long jobs run.
// - Times are ms from the start of the chapter. The first 1200 ms are the camera move, and the pieces the
//   chapter switches on draw in after it; chapter 1, where the loop opens, has no move and draws in at once. A
//   land is where a job's flight from Add() ends, so a chapter's first job lands at 1800 or later. Only chapter
//   2's TunePool(3) comes as early as 1300.
// - Job ids are unique across the loop: the renderer makes one element per id, and a store job's record
//   shares its job's id. Each subscriber makes its own job from a published value, `<value>-<worker>`.

import { endState, simulatePool, type JobSpec, type PoolEvent, type PoolPlan } from './pool.ts';
import {
	simulateRedis,
	simulateSqlite,
	storeEnd,
	type Consumer,
	type RedisPlan,
	type SqlitePlan,
	type StoreEvent,
	type StoreJob,
} from './stores.ts';

export type Worker = 'core' | 'result' | 'second';

export type Part =
	| {
			kind: 'pool';
			worker: Worker;
			plan: PoolPlan;
			events: PoolEvent[];
			outcomes?: Record<string, 'value' | 'error'>; // what each job of a result worker hands back
	  }
	| { kind: 'sqlite'; plan: SqlitePlan; events: StoreEvent[] }
	| { kind: 'redis'; plan: RedisPlan; events: StoreEvent[] };

/** One line under the map. `*word*` is green and `!word!` red; a mark may wrap several words. */
export interface Caption {
	at: number;
	until: number; // when it starts to leave
	text: string;
}

export interface Chapter {
	n: number;
	title: string;
	duration: number;
	captions: Caption[];
	parts: Part[];
	published?: { id: string; at: number }[]; // values published to the topic: one square in its lane each
	// Transient chips. `id` names the piece whose place a chip takes: chapter 2's TunePool(3) shows where the
	// tune control switches on in chapter 9.
	chips?: { id: string; label: string; at: number; until: number }[];
}

type PoolPart = Extract<Part, { kind: 'pool' }>;
type Spec = [id: string, at: number, work: number];

/** The event loop's pace (see `PoolPlan`), slowed down so a job is seen waiting before it leaves. */
const PACE = { wake: 400, gap: 300, after: 250 };
// Takes and acks travel as in the stores' own fixtures.
const SQLITE_TRAVEL = { takeTravel: 720, ackTravel: 780 };
const REDIS_TRAVEL = { landAfter: 700, notifyTravel: 420, takeTravel: 720, ackTravel: 730 };
// Chapters 4 and 5 need the whole burst in the lanes before the first dispatch: there the loop looks later.
const LATE = { ...PACE, wake: 900 };

// The core's sources in bind order: the queue from chapter 1, the priority queue from 4, a third queue from 5
// and the topic from 6. The core is a NewWorker with RoundRobin from the start, and chapter 5 shows the chip.
// The result worker has a queue of its own, and the second worker the topic alone.
const QUEUE = { id: 'queue' };
const PRIO = { id: 'prio', priority: true };
const QUEUE_C = { id: 'queue-c' };
const TOPIC = { id: 'topic' };
const BOUND = [QUEUE, PRIO, QUEUE_C, TOPIC];
const R_QUEUE = { id: 'r-queue' };

const queued = (source: string, specs: Spec[]): JobSpec[] =>
	specs.map(([id, at, work]) => ({ id, source, at, work }));
const stored = (specs: Spec[]): StoreJob[] => specs.map(([id, at, work]) => ({ id, at, work }));
/** A subscriber's jobs from the published values: each lands as its value is published. */
const copies = (values: { id: string; at: number }[], worker: Worker, work: number[]): JobSpec[] =>
	values.map((v, i) => ({ id: `${v.id}-${worker}`, source: TOPIC.id, at: v.at, work: work[i] }));

/**
 * What each worker's last plan left, for its next: each is one worker for the whole loop. A pool carries its limit,
 * its idle goroutines (in the pool's order) and its round-robin turn; the three start as workers with concurrency 1
 * and one idle goroutine, the core is tuned up in chapters 2 and 9, and the other two keep their one ring. A store's
 * worker carries its idle goroutines, one after `Start()` at first.
 */
interface Carry {
	pools: Record<Worker, Pick<PoolPlan, 'limit' | 'idle' | 'turn'>>;
	stores: Record<string, number[]>;
}

const start = (): Carry => ({
	pools: {
		core: { limit: 1, idle: [0], turn: 0 },
		result: { limit: 1, idle: [0], turn: 0 },
		second: { limit: 1, idle: [0], turn: 0 },
	},
	stores: { sqlite: [0], b: [0], c: [0] },
});

// A part's plan starts where its worker's last one ended, and the part leaves in `carry` where this one ends.
function pool(carry: Carry, worker: Worker, rest: Omit<PoolPlan, 'limit' | 'idle' | 'turn'>): PoolPart {
	const plan = { ...carry.pools[worker], ...rest };
	const events = simulatePool(plan);
	carry.pools[worker] = endState(plan, events);
	return { kind: 'pool', worker, plan, events };
}

function sqlite(carry: Carry, rest: Omit<SqlitePlan, 'idle'>): Part {
	const plan = { ...rest, idle: carry.stores.sqlite };
	const events = simulateSqlite(plan);
	Object.assign(carry.stores, storeEnd(plan, events));
	return { kind: 'sqlite', plan, events };
}

function redis(carry: Carry, rest: Omit<RedisPlan, 'consumers'> & { consumers: Omit<Consumer, 'idle'>[] }): Part {
	const plan = { ...rest, consumers: rest.consumers.map((c) => ({ ...c, idle: carry.stores[c.id] })) };
	const events = simulateRedis(plan);
	Object.assign(carry.stores, storeEnd(plan, events));
	return { kind: 'redis', plan, events };
}

// In order, from one carry. Each is written as a function so that a plan the simulations refuse names its chapter.
const SCRIPT: ((carry: Carry) => Omit<Chapter, 'n'>)[] = [
	// 1. Concurrency 1: Add() returns at once, and the jobs trickle in, so at most one waits while the goroutine
	// is busy. A burst would be handed over as one batch.
	(carry) => ({
		title: 'One worker',
		duration: 8000,
		captions: [{ at: 300, until: 7400, text: '*Add()* puts jobs in a queue' }],
		parts: [
			pool(carry, 'core', {
				...PACE,
				sources: [QUEUE],
				strategy: 'roundrobin',
				jobs: queued(QUEUE.id, [
					['j0', 1900, 1500],
					['j1', 2800, 1500],
					['j2', 4500, 1500],
				]),
			}),
		],
	}),

	// 2. TunePool(3), then a burst: the pool grows to 3 while jobs wait, and shrinks to one idle goroutine.
	(carry) => ({
		title: 'The pool',
		duration: 9000,
		captions: [
			{ at: 300, until: 4800, text: 'The pool *grows* while jobs wait' },
			{ at: 5300, until: 8400, text: 'and *shrinks* when the queue empties' },
		],
		chips: [{ id: 'ctl-tune', label: 'TunePool(3)', at: 1300, until: 2500 }],
		parts: [
			pool(carry, 'core', {
				...PACE,
				sources: [QUEUE],
				strategy: 'roundrobin',
				controls: [{ at: 1300, op: 'tune', to: 3 }],
				jobs: queued(QUEUE.id, [
					['b0', 1900, 3000],
					['b1', 2000, 1900],
					['b2', 2100, 2000],
					['b3', 2200, 1800],
					['b4', 2300, 1800],
				]),
			}),
		],
	}),

	// 3. A result worker of its own, beside the core: the core has to stay a plain worker to take a topic.
	(carry) => ({
		title: 'Results and errors',
		duration: 6000,
		captions: [{ at: 300, until: 5400, text: 'Jobs hand back a *value* or an *error*' }],
		parts: [
			{
				...pool(carry, 'result', {
					...PACE,
					sources: [R_QUEUE],
					jobs: queued(R_QUEUE.id, [
						['x0', 1900, 1300],
						['x1', 2800, 1300],
					]),
				}),
				outcomes: { x0: 'value', x1: 'error' },
			},
		],
	}),

	// 4. All five land before the event loop looks, so it takes them lowest number first. Only this lane has
	// jobs, so nothing is said about how the worker picks between its queues.
	(carry) => ({
		title: 'Priority',
		duration: 7000,
		captions: [{ at: 300, until: 6400, text: 'The *lowest* number runs first' }],
		parts: [
			pool(carry, 'core', {
				...LATE,
				sources: [QUEUE, PRIO],
				strategy: 'roundrobin',
				jobs: [5, 2, 8, 1, 3].map((priority, i) => ({
					id: `p${priority}`,
					source: PRIO.id,
					at: 1800 + 150 * i,
					work: [1700, 1400, 1350, 2400, 1450][i],
					priority,
				})),
			}),
		],
	}),

	// 5. Jobs wait in two of the three queues, and round robin takes from them in turn, skipping the empty one.
	(carry) => ({
		title: 'Many queues, one worker',
		duration: 7000,
		captions: [{ at: 300, until: 6400, text: '*RoundRobin* picks the next queue' }],
		parts: [
			pool(carry, 'core', {
				...LATE,
				sources: [QUEUE, PRIO, QUEUE_C],
				strategy: 'roundrobin',
				jobs: [
					...queued(QUEUE.id, [
						['q0', 1900, 2000],
						['q1', 2200, 1600],
					]),
					...queued(QUEUE_C.id, [
						['c0', 2050, 1400],
						['c1', 2350, 1500],
					]),
				],
			}),
		],
	}),

	// 6. Each value is published once, and both subscribers make their own job from it. The second worker runs
	// at concurrency 1, so the second value comes once it has taken the first.
	(carry) => {
		const published = [
			{ id: 't0', at: 1900 },
			{ id: 't1', at: 2700 },
		];
		return {
			title: 'Topics',
			duration: 6000,
			captions: [{ at: 300, until: 5400, text: '*Publish* once, every subscriber gets a copy' }],
			published,
			parts: [
				pool(carry, 'core', {
					...PACE,
					sources: BOUND,
					strategy: 'roundrobin',
					jobs: copies(published, 'core', [2200, 2100]),
				}),
				pool(carry, 'second', { ...PACE, sources: [TOPIC], jobs: copies(published, 'second', [1300, 1300]) }),
			],
		};
	},

	// 7. The stores' SQLite fixture, retimed to fit: s0 and s2 are running when the program dies, the restart
	// puts them back at the head of the queue, ahead of s3, and all four are acked in the end. The program stays
	// down 2600 ms: its stage is gone 1350 ms after the crash, so the store holds the frame alone for over a second.
	(carry) => ({
		title: 'SQLite',
		duration: 17000,
		captions: [
			{ at: 300, until: 1700, text: 'SQLite keeps the *queue* in a file' },
			{ at: 2200, until: 3700, text: '*Add()* writes each job to jobs.db' },
			{ at: 4200, until: 7200, text: 'Taken jobs stay *in flight* until acked' },
			{ at: 7700, until: 9100, text: 'The program !dies! mid-job' },
			{ at: 10200, until: 11700, text: 'On restart they go *back* to the queue' },
			{ at: 12200, until: 16500, text: 'and run again. Nothing is *lost*' },
		],
		parts: [
			sqlite(carry, {
				...SQLITE_TRAVEL,
				limit: 2,
				jobs: stored([
					['s0', 2400, 4200],
					['s1', 2600, 800],
					['s2', 2800, 3000],
					['s3', 3000, 1000],
				]),
				firstClaim: 4200,
				crashAt: 7700,
				restartAt: 10300,
				requeueAfter: 900,
				rework: { s0: 1600, s2: 1000 }, // s3 never ran, so it takes its work
			}),
		],
	}),

	// 8. The stores' Redis fixture, 1500 ms later: program A adds six jobs, B and C take them in turn, and the
	// last two wait in the queue until a goroutine's ack lands.
	(carry) => ({
		title: 'Redis',
		duration: 12000,
		captions: [
			{ at: 300, until: 1700, text: 'Redis holds *one queue* for many programs' },
			{ at: 2200, until: 3800, text: 'Program A adds. Redis *notifies* workers' },
			{ at: 4300, until: 5700, text: 'Each job goes to *one* worker' },
			{ at: 6200, until: 11400, text: 'and is *acked* back to Redis' },
		],
		parts: [
			redis(carry, {
				...REDIS_TRAVEL,
				consumers: [
					{ id: 'b', limit: 2 },
					{ id: 'c', limit: 2 },
				],
				jobs: stored([
					['r0', 1800, 2200],
					['r1', 2040, 1900],
					['r2', 2280, 2500],
					['r3', 2520, 2100],
					['r4', 2760, 1700],
					['r5', 3000, 1900],
				]),
			}),
		],
	}),

	// 9. Pause while two jobs run: they finish, and the jobs added meanwhile wait. Resume once nothing runs (a
	// pausing worker refuses it), then TunePool(5) lets the two still waiting start beside the first three.
	(carry) => ({
		title: 'Control',
		duration: 9000,
		captions: [{ at: 300, until: 8400, text: '*Pause*, *resume* and *resize* while it runs' }],
		parts: [
			pool(carry, 'core', {
				...PACE,
				sources: BOUND,
				strategy: 'roundrobin',
				controls: [
					{ at: 2900, op: 'pause' },
					{ at: 4400, op: 'resume' },
					{ at: 5700, op: 'tune', to: 5 },
				],
				jobs: queued(QUEUE.id, [
					['k0', 1900, 1700],
					['k1', 2100, 1300],
					['k2', 3100, 2600],
					['k3', 3300, 2500],
					['k4', 3500, 2300],
					['k5', 3700, 1600],
					['k6', 3900, 1700],
				]),
			}),
		],
	}),

	// 10. The whole map runs a little at once, and the core is stopped while its jobs still run: they finish,
	// and then its goroutines go. Nothing waits in its queue at the stop.
	(carry) => {
		const published = [{ id: 'zt0', at: 2200 }];
		return {
			title: 'Everything together',
			duration: 8000,
			captions: [
				{ at: 300, until: 3800, text: 'One worker model, from memory to *Redis*' },
				{ at: 4300, until: 7500, text: '*Stop()* lets running jobs finish' },
			],
			published,
			parts: [
				pool(carry, 'core', {
					...PACE,
					sources: BOUND,
					strategy: 'roundrobin',
					controls: [{ at: 4400, op: 'stop' }],
					jobs: [
						...queued(QUEUE.id, [
							['zj0', 2000, 2200],
							['zj1', 2400, 2600],
						]),
						...copies(published, 'core', [2400]),
					],
				}),
				pool(carry, 'second', { ...PACE, sources: [TOPIC], jobs: copies(published, 'second', [1500]) }),
				{
					...pool(carry, 'result', {
						...PACE,
						sources: [R_QUEUE],
						jobs: queued(R_QUEUE.id, [
							['zx0', 1900, 1400],
							['zx1', 2900, 1400],
						]),
					}),
					outcomes: { zx0: 'value', zx1: 'error' },
				},
				sqlite(carry, {
					...SQLITE_TRAVEL,
					limit: 2,
					jobs: stored([
						['zs0', 2000, 1800],
						['zs1', 2200, 2400],
					]),
					firstClaim: 2800,
				}),
				redis(carry, {
					...REDIS_TRAVEL,
					consumers: [
						{ id: 'b', limit: 2 },
						{ id: 'c', limit: 2 },
					],
					jobs: stored([
						['zr0', 1600, 1800],
						['zr1', 1800, 1700],
						['zr2', 2000, 1900],
						['zr3', 2200, 1600],
					]),
				}),
			],
		};
	},
];

/** The chapters the script writes, each from the carry the ones before it leave. */
function write(script: typeof SCRIPT): Chapter[] {
	const carry = start();
	return script.map((chapter, i) => {
		try {
			return { n: i + 1, ...chapter(carry) };
		} catch (e) {
			throw new Error(`chapter ${i + 1}: ${(e as Error).message}`, { cause: e });
		}
	});
}

export const CHAPTERS: Chapter[] = write(SCRIPT);

/** When each chapter starts, ms from the start of the loop. */
export function starts(): number[] {
	const at: number[] = [];
	let t = 0;
	for (const c of CHAPTERS) {
		at.push(t);
		t += c.duration;
	}
	return at;
}

export const TOTAL = CHAPTERS.reduce((t, c) => t + c.duration, 0);

/** The still frame for reduced motion: the finale once the camera has pulled back, before anything moves. */
export const STATIC_AT = starts()[CHAPTERS.length - 1] + 1300;

/** A caption's text without its colour marks. */
export function plain(text: string): string {
	return text.replace(/[*!]/g, '');
}
