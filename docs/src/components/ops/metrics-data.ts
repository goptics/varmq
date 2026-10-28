// The eight collectors of the Prometheus example (examples/prometheus) and the numbers of its run.
//
// sample() reproduces a real run of that program on varmq: a worker with concurrency 10, 1000 jobs
// that each sleep 100 ms. A scratch Go program read NumPending, NumProcessing, NumIdleWorkers and the
// four counters every 500 ms. Completed was 40, 90, 140, ... 990 at 0.5 s, 1.0 s, 1.5 s, ... 10.0 s,
// then 1000 with the worker Idle and one idle goroutine. 10 * floor(10 t) - 10 fits all of them.

export interface Sample {
	completed: number;
	successful: number;
	failed: number;
	submitted: number;
	waiting: number;
	processing: number;
	concurrency: number;
	idle: number;
}

export const END = 11; // seconds of the replay

export function sample(t: number, restartAt: number | null): Sample {
	const tt = restartAt !== null && t >= restartAt ? t - restartAt : t;
	const completed = Math.min(1000, Math.max(0, 10 * Math.floor(tt * 10 + 1e-6) - 10));
	const processing = completed >= 1000 ? 0 : 10;
	return {
		completed,
		successful: completed,
		failed: 0,
		submitted: 1000,
		waiting: Math.max(0, 1000 - completed - processing),
		processing,
		concurrency: 10,
		idle: completed >= 1000 ? 1 : 0,
	};
}

export interface Metric {
	key: keyof Sample;
	name: string;
	type: 'gauge' | 'counter';
	help: string;
	from: string;
	href: string;
	meaning: string;
	watch?: string;
	queries: { q: string; says: string }[];
}

const doc = 'https://pkg.go.dev/github.com/goptics/varmq#';

/** In the order client_golang prints them: by name. */
export const METRICS: Metric[] = [
	{
		key: 'completed',
		name: 'completed_total',
		type: 'counter',
		help: 'Jobs whose function has returned',
		from: 'Metrics().Completed',
		href: doc + 'Metrics.Completed',
		meaning: 'Jobs whose function has returned, whether it succeeded or failed. It equals successful plus failed.',
		watch: 'It starts at zero when the worker is created. A restart of the process zeroes it, and rate() reads the drop as a counter reset.',
		queries: [
			{ q: 'rate(foo_worker_completed_total[5m])', says: 'Jobs finished per second, averaged over 5 minutes. Here 10 jobs finish every 100 ms, so about 100 per second.' },
			{ q: 'increase(foo_worker_completed_total[1h])', says: 'How many jobs finished in the last hour.' },
		],
	},
	{
		key: 'failed',
		name: 'failed_total',
		type: 'counter',
		help: 'Jobs that returned an error or panicked',
		from: 'Metrics().Failed',
		href: doc + 'Metrics.Failed',
		meaning: 'Jobs that returned an error or panicked. This run has none.',
		watch: 'Reset() zeroes it, and Prometheus reads that as a reset, like a restart. Do not call Reset on a worker you export unless you want that.',
		queries: [
			{ q: 'rate(foo_worker_failed_total[5m]) / rate(foo_worker_completed_total[5m])', says: 'The share of finished jobs that failed. It is NaN while no job finishes.' },
			{ q: 'increase(foo_worker_failed_total[1h]) > 0', says: 'Shows the worker only if a job failed in the last hour.' },
		],
	},
	{
		key: 'submitted',
		name: 'submitted_total',
		type: 'counter',
		help: 'Jobs the queue accepted',
		from: 'Metrics().Submitted',
		href: doc + 'Metrics.Submitted',
		meaning: 'Jobs the queue accepted. The program adds all 1000 at once, so this reads 1000 from the first scrape.',
		watch: 'On a persistent queue it counts this process’s accepted Add calls only. A distributed queue’s count may differ from what you expect.',
		queries: [{ q: 'rate(foo_worker_submitted_total[5m])', says: 'Jobs accepted per second. Compare it with the completed rate: if it is higher for long, the queue grows.' }],
	},
	{
		key: 'successful',
		name: 'successful_total',
		type: 'counter',
		help: 'Jobs that returned without an error or a panic',
		from: 'Metrics().Successful',
		href: doc + 'Metrics.Successful',
		meaning: 'Jobs that returned without an error or a panic.',
		queries: [{ q: 'rate(foo_worker_successful_total[5m])', says: 'Jobs that finished well, per second.' }],
	},
	{
		key: 'waiting',
		name: 'waiting_total',
		type: 'gauge',
		help: 'Jobs not started yet; can read high while batches run',
		from: 'Worker.NumPending',
		href: doc + 'Worker.NumPending',
		meaning: 'Jobs that have not started: they wait in the queue, or a goroutine took them in a batch and has not started them.',
		watch: 'It is a gauge. It goes up and down, although the _total suffix is the Prometheus convention for counters. On a persistent or distributed queue, each scrape calls the storage.',
		queries: [
			{ q: 'foo_worker_waiting_total', says: 'The backlog now.' },
			{ q: 'avg_over_time(foo_worker_waiting_total[5m]) > 500', says: 'True when the backlog averaged above 500 jobs for 5 minutes. Pick your own limit.' },
		],
	},
	{
		key: 'concurrency',
		name: 'workers_concurrency',
		type: 'gauge',
		help: 'Maximum number of jobs processed at once',
		from: 'Worker.NumConcurrency',
		href: doc + 'Worker.NumConcurrency',
		meaning: 'The concurrency limit. It changes when you call TunePool, or when a request changes it through the REST API.',
		queries: [{ q: 'foo_worker_workers_processing / foo_worker_workers_concurrency', says: 'How full the pool is: 1 means every slot runs a job.' }],
	},
	{
		key: 'idle',
		name: 'workers_idle',
		type: 'gauge',
		help: 'Idle goroutines in the pool',
		from: 'Worker.NumIdleWorkers',
		href: doc + 'Worker.NumIdleWorkers',
		meaning: 'Idle goroutines in the pool. It reads 0 while every goroutine runs a job, and 1 after this run ends.',
		queries: [{ q: 'foo_worker_workers_idle', says: 'Goroutines that wait for a job now.' }],
	},
	{
		key: 'processing',
		name: 'workers_processing',
		type: 'gauge',
		help: 'Jobs being processed right now',
		from: 'Worker.NumProcessing',
		href: doc + 'Worker.NumProcessing',
		meaning: 'Jobs being processed right now. It never exceeds the concurrency limit.',
		queries: [
			{ q: 'foo_worker_workers_processing', says: 'Jobs that run now.' },
			{ q: 'max_over_time(foo_worker_workers_processing[1h])', says: 'The most jobs that ran at once in the last hour.' },
		],
	},
];
