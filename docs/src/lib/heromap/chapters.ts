// The hero map's story: ten chapters, each a camera stop with its captions and the simulations that play
// there. Pure data for the renderer and the choreographer, which draw and animate exactly these events.
// Every land, dispatch, take and ack comes out of the pool and store simulations (`pool.ts`, `stores.ts`),
// so the map can only show what varmq does; the plans here choose when things happen and how long jobs run.
// - Times are ms from the start of the chapter. The first 1200 ms are the camera move, and the pieces the
//   chapter switches on draw in from 300 ms, as the view sweeps toward them. Chapter 1, where the loop opens, has
//   no move: its pieces are on from the start, as the finale's last 1200 ms, a zoom into its frame, leave them. A
//   land is where a job's flight from Add() ends, so a chapter's first job lands at 1800 or later. Only chapter
//   2's TunePool(5) comes as early as 1300.
// - Job ids are unique across the loop: the renderer makes one element per id, and a store job's record
//   shares its job's id. Each subscriber makes its own job from a published value, `<value>-<worker>`.

import { endState, simulatePool, type JobSpec, type PoolEvent, type PoolPlan, type Strategy } from './pool.ts';
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

/**
 * One line under the map. `*word*` is green and `!word!` red, and `#word#` takes its logo's colour (SQLite's or
 * Redis's); a mark may wrap several words.
 */
export interface Caption {
	at: number;
	until: number; // when it starts to leave
	text: string;
	swap?: { at: number; text: string }; // at `at`, the last word rolls up out of sight as `text` rolls up into its place
}

export interface Chapter {
	n: number;
	title: string;
	duration: number;
	captions: Caption[];
	parts: Part[];
	published?: { id: string; at: number }[]; // values published to the topic: one square in its lane each
	// Transient chips. `id` names the piece whose place a chip takes: chapter 2's TunePool(5) sits where Restart()
	// switches on in chapter 9. A chip pops in, or with `fade` takes the place of the chip before it at once while
	// its name fades in, as chapter 5's strategy names take over from each other; one that leaves under another is
	// not seen to.
	chips?: { id: string; label: string; at: number; until: number; fade?: boolean }[];
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
// Chapter 5's four bursts must each land, run and finish before the next lands, so there it runs a little quicker.
const VOLLEY = { ...PACE, wake: 400, gap: 200 };

// The core's sources in bind order: the queue from chapter 1 (alerts), the priority queue from 4 (reports), a third
// queue from 5 (emails) and the topic from 6. None is bound with a priority, so the worker holds them in that order. The
// core is a NewWorker with RoundRobin from the start, and chapter 5 shows the chip, after showing what each of the
// other strategies would do. The result worker has a queue of its own, and the second worker the topic alone.
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
 * and one idle goroutine, the core is tuned up to 5 in chapter 2, and the other two keep their one ring. A store's
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
		duration: 8300,
		captions: [{ at: 300, until: 7400, text: '*Add* puts jobs in a queue' }],
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

	// 2. TunePool(5), then a burst: the pool grows to 5 while jobs wait, and shrinks to one idle goroutine as they
	// finish, under the second caption.
	(carry) => ({
		title: 'The pool',
		duration: 9000,
		captions: [
			{ at: 300, until: 4800, text: 'The pool *grows* while jobs wait' },
			{ at: 5300, until: 8400, text: 'and *shrinks* when the queue empties' },
		],
		chips: [{ id: 'ctl-restart', label: 'TunePool(5)', at: 1300, until: 2500 }],
		parts: [
			pool(carry, 'core', {
				...PACE,
				sources: [QUEUE],
				strategy: 'roundrobin',
				controls: [{ at: 1300, op: 'tune', to: 5 }],
				jobs: queued(QUEUE.id, [
					['b0', 1900, 3800],
					['b1', 2000, 3300],
					['b2', 2100, 3600],
					['b3', 2200, 3600],
					['b4', 2300, 3700],
				]),
			}),
		],
	}),

	// 3. A result worker of its own, beside the core: the core has to stay a plain worker to take a topic.
	(carry) => ({
		title: 'Results and errors',
		duration: 6200,
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

	// 5. Each strategy in turn, on the same three queues. A burst lands in them a second after its caption, before the
	// event loop looks, so the order its jobs leave in is the strategy's: Priority goes in bind order, as no queue is
	// bound with a priority, so it empties alerts first though reports is longer and emails shorter; MaxLen takes from
	// the longest, MinLen from the shortest, and RoundRobin from each in turn. The counts keep each burst's first
	// picks free of ties. A worker has one strategy for life (`WithStrategy`): this shows what each would do, and ends
	// on RoundRobin, which the worker keeps.
	(carry) => {
		const bursts: [Strategy, number, Record<string, number>][] = [
			['priority', 300, { [QUEUE.id]: 2, [PRIO.id]: 3, [QUEUE_C.id]: 1 }],
			['maxlen', 4650, { [QUEUE.id]: 1, [PRIO.id]: 3, [QUEUE_C.id]: 1 }],
			['minlen', 9000, { [QUEUE.id]: 3, [PRIO.id]: 1, [QUEUE_C.id]: 2 }],
			['roundrobin', 13350, { [QUEUE.id]: 2, [PRIO.id]: 2, [QUEUE_C.id]: 2 }],
		];
		// A burst's jobs land 60 ms apart, taking the three lanes in turn, all before the loop looks 400 ms on. Each
		// burst, the last check included, is done before the next caption, which comes 4350 ms after its own.
		const jobs = bursts.flatMap(([, at, counts], b) => {
			const left = { ...counts };
			const order: string[] = [];
			while (order.length < Object.values(counts).reduce((n, c) => n + c, 0)) {
				for (const id of [QUEUE.id, PRIO.id, QUEUE_C.id]) if (left[id]-- > 0) order.push(id);
			}
			return order.map((source, k) => ({ id: `st${b}.${k}`, source, at: at + 1000 + 60 * k, work: [650, 750, 700, 800, 650, 750][k] }));
		});
		return {
			title: 'Many queues, one worker',
			duration: 17900,
			captions: [
				{ at: 300, until: 4150, text: '*Priority*, the default, goes in bind order' },
				{ at: 4650, until: 8500, text: '*MaxLen* takes from the longest queue' },
				{ at: 9000, until: 12850, text: '*MinLen* takes from the shortest queue' },
				{ at: 13350, until: 17100, text: '*RoundRobin* takes from each queue in turn' },
			],
			// Each strategy's name takes the chip in turn. Priority pops in with the chip, and no later, so the chip's own
			// RoundRobin never shows beneath it; each after it covers the one before and fades its name in. RoundRobin
			// does too, and gives way to the chip's own, the same, as the chapter ends.
			chips: [
				{ id: 'strategy', label: 'Priority', at: 300, until: 5250 },
				{ id: 'strategy', label: 'MaxLen', at: 4650, until: 9600, fade: true },
				{ id: 'strategy', label: 'MinLen', at: 9000, until: 13950, fade: true },
				{ id: 'strategy', label: 'RoundRobin', at: 13350, until: 17900, fade: true },
			],
			parts: [
				pool(carry, 'core', {
					...VOLLEY,
					sources: [QUEUE, PRIO, QUEUE_C],
					strategy: bursts.map(([strategy, at]) => [at, strategy]),
					jobs,
				}),
			],
		};
	},

	// 6. Each value is published once, and both subscribers get it at once and make their own job from it. The
	// second worker runs at concurrency 1, so its copy of the second value waits at it until the first is done.
	(carry) => {
		const published = [
			{ id: 't0', at: 1900 },
			{ id: 't1', at: 2700 },
		];
		return {
			title: 'Topics',
			duration: 6200,
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
	// down 2600 ms, its box red and its card empty. The first caption ends as the store has drawn in (the camera
	// lands at 1200), and the rerun waits for the last caption, so the one before it ends on the jobs in the queue.
	(carry) => ({
		title: 'SQLite',
		duration: 16900,
		captions: [
			{ at: 300, until: 1150, text: 'SQLite keeps the *queue* in a file' },
			{ at: 1300, until: 2800, text: '*Add* writes each job to jobs.db' },
			{ at: 3300, until: 6300, text: 'Taken jobs stay *in flight* until acked' },
			{ at: 6800, until: 8200, text: 'The program !dies! mid-job' },
			{ at: 9300, until: 10400, text: 'On restart they go *back* to the queue' },
			{ at: 10900, until: 16200, text: 'and run again. Nothing is *lost*' },
		],
		parts: [
			sqlite(carry, {
				...SQLITE_TRAVEL,
				limit: 2,
				jobs: stored([
					['s0', 1900, 4200],
					['s1', 2100, 800],
					['s2', 2300, 3000],
					['s3', 2500, 1000],
				]),
				firstClaim: 3300,
				crashAt: 6800,
				restartAt: 9400,
				requeueAfter: 900,
				rerunAfter: 600, // the jobs wait in the queue for the last caption, which takes them
				rework: { s0: 1600, s2: 1000 }, // s3 never ran, so it takes its work
			}),
		],
	}),

	// 8. Program A adds four jobs, and Redis wakes B and C, the two idle workers, once each: each pulls two. B dies
	// mid-job, and its jobs stay in flight in Redis while C finishes its own and goes idle. Then Redis takes B for
	// dead, and the sweep puts B's jobs back at the front and wakes C, which takes both at once. (In redisq that is
	// about six seconds after the crash; here 2.5 s.) B comes back at the end, with nothing left to take. The first
	// caption ends as Redis has drawn in, and the one before the last ends on B's jobs back in the queue: the wake
	// comes 400 ms later, with the last.
	(carry) => ({
		title: 'Redis',
		duration: 11500,
		captions: [
			{ at: 300, until: 1150, text: 'Redis holds *one queue* for many programs' },
			{ at: 1300, until: 3300, text: 'Program A adds. Redis *notifies* workers' },
			{ at: 3800, until: 5400, text: 'Program B !dies! mid-job' },
			{ at: 5900, until: 6800, text: 'Its jobs go *back* to the front' },
			{ at: 6900, until: 10800, text: 'Redis wakes C, which runs them. Nothing is *lost*' },
		],
		parts: [
			redis(carry, {
				...REDIS_TRAVEL,
				consumers: [
					{ id: 'b', limit: 2 },
					{ id: 'c', limit: 2 },
				],
				jobs: stored([
					['r0', 1300, 2200],
					['r1', 1400, 1900],
					['r2', 1500, 1400],
					['r3', 1600, 1000],
				]),
				crash: { consumer: 'b', at: 4000, backAfter: 2500, wakeAfter: 400, restartAt: 9000, rework: { r0: 1600, r1: 1300 } },
			}),
		],
	}),

	// 9. Pause while two jobs run: they finish, and the two added meanwhile wait. Resume once nothing runs (a
	// pausing worker refuses it), and they start. A third is added while they run, and Stop comes as it is due: the
	// two running finish and every goroutine goes, and the third waits with the two added while the worker is
	// stopped. Restart starts it again with one idle goroutine, and the three run. Each press starts its caption.
	(carry) => ({
		title: 'Control',
		duration: 11600,
		captions: [
			{ at: 2300, until: 3500, text: '*Pause* holds new jobs back' },
			{ at: 4000, until: 5600, text: '*Resume* starts the jobs that waited' },
			{ at: 6100, until: 7400, text: '*Stop* ends the loop once jobs finish' },
			{ at: 7900, until: 10800, text: '*Restart* runs the jobs that waited' },
		],
		parts: [
			pool(carry, 'core', {
				...PACE,
				sources: BOUND,
				strategy: 'roundrobin',
				controls: [
					{ at: 2300, op: 'pause' },
					{ at: 4000, op: 'resume' },
					{ at: 6100, op: 'stop' },
					{ at: 7900, op: 'restart' },
				],
				jobs: queued(QUEUE.id, [
					['k0', 1300, 1300],
					['k1', 1500, 1000],
					['k2', 2500, 2400],
					['k3', 2700, 2200],
					['k4', 5800, 2000],
					['k5', 6700, 1600],
					['k6', 6900, 1500],
				]),
			}),
		],
	}),

	// 10. The whole map runs a little at once, and the core is stopped while its jobs still run: they finish,
	// and then its goroutines go. Nothing waits in its queue at the stop. The one caption stays up until the map is
	// still, its last word SQLite giving way to Redis halfway; the stop only winds the map down. From 7800 ms the
	// camera zooms into chapter 1's frame as everything else fades, and the loop starts over.
	(carry) => {
		const published = [{ id: 'zt0', at: 2200 }];
		return {
			title: 'Everything together',
			duration: 9000,
			captions: [
				{ at: 300, until: 7500, text: 'One worker model, from memory to #SQLite#', swap: { at: 3900, text: '#Redis#' } },
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

/** Each chapter's camera move, its first CAMERA ms, and the finale's zoom into chapter 1's frame, its last. */
export const CAMERA = 1200;

/** The still frame for reduced motion: the finale once the camera has pulled back, before anything moves. */
export const STATIC_AT = starts()[CHAPTERS.length - 1] + 1300;

/** A caption's text without its colour marks. */
/** A transient chip's element: chapter n's chip i, in its piece's place. A chapter may have several in one place. */
export const chipKey = (n: number, i: number, id: string) => `${id}@${n}:${i}`;

export function plain(text: string): string {
	return text.replace(/[*!#]/g, '');
}
