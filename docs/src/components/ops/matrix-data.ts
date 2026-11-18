// The feature matrix of the Comparison page. The cells are the ones of the page as it was checked against
// each package's source in October 2026: none is changed here. `limit` is new, and says where a varmq
// "Yes" has a limit, in the words of varmq's own pages.

export const LIBS = ['varmq', 'ants', 'pond', 'conc', 'workerpool', 'errgroup', 'gopool'] as const;
export type Lib = (typeof LIBS)[number];

export const LIB_LINKS: Record<Lib, string> = {
	varmq: 'https://github.com/goptics/varmq',
	ants: 'https://github.com/panjf2000/ants',
	pond: 'https://github.com/alitto/pond',
	conc: 'https://github.com/sourcegraph/conc',
	workerpool: 'https://github.com/gammazero/workerpool',
	errgroup: 'https://pkg.go.dev/golang.org/x/sync/errgroup',
	gopool: 'https://github.com/bytedance/gopkg/tree/main/util/gopool',
};

export type Group = 'API' | 'Queues' | 'Lifecycle' | 'Operations';
export const GROUPS: Group[] = ['API', 'Queues', 'Lifecycle', 'Operations'];

/** `Yes` or `No`, and the number of the footnote when the page has one. */
type Cell = [v: 'Yes' | 'No', note?: 1 | 2 | 3 | 4];

export interface Row {
	feature: string;
	group: Group;
	cells: Record<Lib, Cell>;
	/** Where varmq's own docs say its "Yes" has a limit. */
	limit?: { text: string; href: string; source: string };
}

const row = (feature: string, group: Group, c: Cell[], limit?: Row['limit']): Row => ({
	feature,
	group,
	cells: Object.fromEntries(LIBS.map((l, i) => [l, c[i]])) as Record<Lib, Cell>,
	limit,
});

const Y: Cell = ['Yes'];
const N: Cell = ['No'];

export const ROWS: Row[] = [
	row(
		'Generic (type-safe) API',
		'API',
		[Y, Y, Y, Y, N, N, N],
		{
			text: 'Shared storage keeps each job as JSON, so the job type has to work with encoding/json. Function helpers do not work there.',
			href: '/persistence/overview/#what-is-stored',
			source: 'What is stored',
		},
	),
	row(
		'Tasks return results and errors',
		'API',
		[Y, N, Y, Y, N, ['No', 2], N],
		{
			text: 'On shared storage, the handle gets the outcome only if the storage implements OutcomeWatcher. redisq and sqliteq do. A result over 64 KiB is dropped, and an error comes back as its text only.',
			href: '/concepts/jobs/#jobs-in-other-processes',
			source: 'Jobs in other processes',
		},
	),
	row(
		'Task groups (submit many, wait once)',
		'API',
		[Y, N, Y, Y, N, Y, N],
		{
			text: 'AddAll works on in-memory queues and on shared storage.',
			href: '/persistence/overview/#what-is-stored',
			source: 'Persistence overview',
		},
	),
	row('Priority queue', 'Queues', [Y, N, N, N, N, N, N]),
	row(
		'Persistent queues (survive restarts)',
		'Queues',
		[Y, N, N, N, N, N, N],
		{
			text: 'VarMQ has no adapter of its own: you add redisq (Redis) or sqliteq (SQLite). The best case is at-least-once delivery, so a job can run twice.',
			href: '/persistence/overview/#what-this-guarantees',
			source: 'What this guarantees',
		},
	),
	row(
		'Distributed queues (across instances)',
		'Queues',
		[Y, N, N, N, N, N, N],
		{
			text: 'Every redisq queue serves many processes and wakes them when a job is added. sqliteq serves only the processes on one host, through one file, and an idle worker checks it about once a second.',
			href: '/persistence/overview/#persistent-or-distributed',
			source: 'Persistent or distributed',
		},
	),
	row('Several queues bound to one pool', 'Queues', [Y, N, ['Yes', 4], N, N, N, N]),
	row(
		'Topic fan-out (publish once, many pools)',
		'Queues',
		[Y, N, N, N, N, N, N],
		{
			text: 'Topics are in-memory only, and only NewWorker can subscribe, so a topic job has no result or error. To share jobs between processes, use shared storage.',
			href: '/guides/topics/',
			source: 'Topics',
		},
	),
	row(
		'Bounded queue, reject when full',
		'Queues',
		[Y, Y, Y, N, N, ['No', 3], N],
		{
			text: 'Not with redisq or sqliteq. Neither implements CapacitySetter, so Add is not refused on them. An adapter of your own can implement it.',
			href: '/persistence/overview/#what-is-stored',
			source: 'Persistence overview',
		},
	),
	row(
		'Pause and resume',
		'Lifecycle',
		[Y, N, N, N, Y, N, N],
		{
			text: 'On shared storage, Pause and Stop do not park a job the worker already took. It runs.',
			href: '/persistence/overview/#acknowledgement',
			source: 'Acknowledgement',
		},
	),
	row(
		'Restart after stop',
		'Lifecycle',
		[Y, Y, N, N, N, N, N],
		{
			text: 'Not after the worker’s context is cancelled: a done context stays done. Create a new worker instead.',
			href: '/guides/context/#starting-again-after-a-cancel',
			source: 'Context',
		},
	),
	row('Resize pool at runtime', 'Lifecycle', [Y, Y, Y, N, N, N, Y]),
	row(
		'Cancel work via context',
		'Lifecycle',
		[Y, N, Y, Y, N, Y, N],
		{
			text: 'VarMQ cannot stop a function. Cancelling the context shuts the worker down, and a running job stops only if it watches the worker’s Context(). To stop one job, call its Cancel and watch j.Context().',
			href: '/concepts/jobs/#cancelling-a-job',
			source: 'Cancelling a job',
		},
	),
	row('Panic recovery', 'Lifecycle', [Y, Y, Y, ['No', 1], N, N, Y]),
	row('Task counters (completed, failed, ...)', 'Operations', [Y, N, Y, N, N, N, N]),
	row(
		'Built-in REST API',
		'Operations',
		[Y, N, N, N, N, N, N],
		{
			text: 'It has no authentication. Put it behind middleware or a reverse proxy before you expose it.',
			href: '/operations/rest-api/',
			source: 'REST API',
		},
	),
];

export const NOTES: string[] = [
	'conc re-raises a task’s panic from `Wait` by design.',
	'errgroup returns the first error, no results.',
	'errgroup has no queue; `TryGo` only refuses a task while every slot is busy.',
	'pond subpools: each has its own queue and runs on the parent pool’s workers.',
];
