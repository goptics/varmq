// The home page's proof, copied from the README's "Comparison and Benchmarks" section: the feature table as checked
// against each package's source in October 2026, and the benchmark run of 2026-10-25. Change it with the README.

export const BENCHMARKS = 'https://github.com/goptics/benchmarks';
export const CHARTS = 'https://benchmarks.goptics.org';
export const COMPARISON = 'https://github.com/goptics/varmq#comparison-and-benchmarks';

export const POOLS = ['VarMQ', 'ants', 'pond', 'conc', 'workerpool', 'errgroup', 'gopool'] as const;

/** yes, no, or a footnote number for a qualified yes or no. */
export type Mark = 'yes' | 'no' | { yes: boolean; note: number };

export const FEATURES: { feature: string; marks: Mark[] }[] = [
	{ feature: 'Priority queue', marks: ['yes', 'no', 'no', 'no', 'no', 'no', 'no'] },
	{ feature: 'Persistent queues, which survive restarts', marks: ['yes', 'no', 'no', 'no', 'no', 'no', 'no'] },
	{ feature: 'Distributed queues, shared across instances', marks: ['yes', 'no', 'no', 'no', 'no', 'no', 'no'] },
	{ feature: 'Topic fan-out', marks: ['yes', 'no', 'no', 'no', 'no', 'no', 'no'] },
	{ feature: 'Several queues bound to one pool', marks: ['yes', 'no', { yes: true, note: 2 }, 'no', 'no', 'no', 'no'] },
	{ feature: 'Tasks return results and errors', marks: ['yes', 'no', 'yes', 'yes', 'no', { yes: false, note: 1 }, 'no'] },
	{ feature: 'Pause and resume', marks: ['yes', 'no', 'no', 'no', 'yes', 'no', 'no'] },
	{ feature: 'Resize the pool at runtime', marks: ['yes', 'yes', 'yes', 'no', 'no', 'no', 'yes'] },
	{ feature: 'Built-in REST API', marks: ['yes', 'no', 'no', 'no', 'no', 'no', 'no'] },
];

export const FEATURE_NOTES = [
	'errgroup returns the first error, and no results.',
	"pond's subpools each have their own queue and run on the parent pool's workers.",
];

export const RUN = 'Run of 2026-10-25 on an i7-13700 (24 threads), Go 1.26.';

/** 15M tasks on 50K workers: seconds at 4 and 24 CPUs, million tasks a second, MB per 1M tasks. */
export const POOL_RUNS: { pool: string; cpu4: number; cpu24: number; rate: number; mb: number; handRolled?: true }[] = [
	{ pool: 'VarMQ', cpu4: 5.07, cpu24: 3.85, rate: 4.89, mb: 115 },
	{ pool: 'BufferedPool', cpu4: 5.43, cpu24: 8.49, rate: 2.73, mb: 23, handRolled: true },
	{ pool: 'errgroup', cpu4: 6.38, cpu24: 9.23, rate: 2.29, mb: 111 },
	{ pool: 'ants', cpu4: 6.74, cpu24: 8.0, rate: 2.22, mb: 15 },
	{ pool: 'gopool', cpu4: 7.13, cpu24: 11.0, rate: 1.92, mb: 52 },
	{ pool: 'GoroutinePool', cpu4: 7.24, cpu24: 12.85, rate: 1.9, mb: 14, handRolled: true },
	{ pool: 'conc', cpu4: 8.45, cpu24: 17.38, rate: 1.56, mb: 15 },
	{ pool: 'workerpool', cpu4: 10.2, cpu24: 16.42, rate: 1.35, mb: 28 },
	{ pool: 'pond v2', cpu4: 20.67, cpu24: 42.38, rate: 0.6, mb: 318 },
];

/** 100K no-op jobs from 100 producers, jobs a second with 100 workers in one instance and in four. */
export const REDIS_RUNS = [
	{ library: 'VarMQ', c1: 59898, c4: 64352 },
	{ library: 'gocraft/work', c1: 44775, c4: 42707 },
	{ library: 'taskq', c1: 34499, c4: 15898 },
	{ library: 'rmq', c1: 28521, c4: 59094 },
	{ library: 'Asynq', c1: 17744, c4: 17670 },
];

/** 20K jobs, jobs a second with produces and consumes at once, and draining a full queue. */
export const SQLITE_RUNS = [
	{ library: 'VarMQ', mixed: 8575, drain: 14725 },
	{ library: 'goqite', mixed: 3032, drain: 2933 },
	{ library: 'River', mixed: 1375, drain: 3230 },
	{ library: 'backlite', mixed: 924, drain: 1119 },
];
