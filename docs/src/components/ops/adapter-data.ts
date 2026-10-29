// The storage interfaces of varmq, as data for the adapter map. Every sentence comes from the
// Adapters page and from interface.go, source.go and worker_binder.go of the library.

export type Caps = { fifo: boolean; prio: boolean; ack: boolean; sub: boolean; batch: boolean; dedup: boolean; cap: boolean };

export const NO_CAPS: Caps = { fifo: false, prio: false, ack: false, sub: false, batch: false, dedup: false, cap: false };

export interface NodeDef {
	id: string;
	/** Column: 0 is the FIFO queue, 1 the priority queue, 2 the parts both can have. */
	col: 0 | 1 | 2;
	/** Row, from the root of the family to the most specific interface. */
	row: number;
	kind: 'base' | 'block' | 'composite' | 'option';
	/** What a composite is made of, shown under its name. */
	parts?: string;
	on: (c: Caps) => boolean;
	/** Click on a node that is off: switch on what it needs. */
	enable: (c: Caps) => void;
	/** Click on a block that is on: switch it off. Composites have none. */
	disable?: (c: Caps) => void;
	methods: string[];
	adds: string;
	worker: string[];
	bind?: string;
	by: ('redisq' | 'sqliteq')[];
}

const useFifo = (c: Caps) => {
	c.fifo = true;
	c.prio = false;
};
const usePrio = (c: Caps) => {
	c.prio = true;
	c.fifo = false;
};

export const BASE: NodeDef = {
	id: 'IBaseQueue',
	col: 2,
	row: -1,
	kind: 'base',
	on: () => true,
	enable: () => {},
	methods: ['Len() int', 'Dequeue() (any, bool)', 'Values() []any', 'Purge()', 'Close() error'],
	adds: 'The root of every queue. Every queue you hand to varmq has these five methods.',
	worker: [
		'Len: whenever it decides whether there is work. On a persistent or distributed queue each call is a call to the storage.',
		'Dequeue: once for each job it takes from a queue that does not acknowledge.',
		'Values and Purge: when you call Purge on the handle. Close: when you call Close on the handle.',
	],
	by: ['redisq', 'sqliteq'],
};

export const NODES: NodeDef[] = [
	{
		id: 'IQueue',
		col: 0,
		row: 0,
		kind: 'block',
		on: (c) => c.fifo,
		enable: useFifo,
		disable: (c) => {
			c.fifo = false;
			c.dedup = c.dedup && c.prio;
		},
		methods: ['Enqueue(item any) bool'],
		adds: 'Adds Enqueue to IBaseQueue. This is how a job gets in.',
		worker: [
			'The handle’s Add calls Enqueue once for each job. It calls EnqueueOnce instead when the job has an ID and the queue is an IDedupQueue.',
			'true means the item is stored. false makes Add return false.',
		],
		bind: 'WithQueue',
		by: ['redisq', 'sqliteq'],
	},
	{
		id: 'IPriorityQueue',
		col: 1,
		row: 0,
		kind: 'block',
		on: (c) => c.prio,
		enable: usePrio,
		disable: (c) => {
			c.prio = false;
			c.dedup = c.dedup && c.fifo;
		},
		methods: ['Enqueue(item any, priority int) bool'],
		adds: 'Adds Enqueue with a priority to IBaseQueue. A type has one Enqueue, so it is a FIFO queue or a priority queue, not both.',
		worker: [
			'The priority is the number the caller passed to Add. varmq does not order the queue.',
			'The worker takes whatever Dequeue returns next, so a priority queue has to hand out the lowest number first itself.',
		],
		bind: 'WithPriorityQueue',
		by: ['redisq', 'sqliteq'],
	},
	{
		id: 'IAcknowledgeable',
		col: 2,
		row: 0,
		kind: 'block',
		on: (c) => c.ack,
		enable: (c) => {
			c.ack = true;
		},
		disable: (c) => {
			c.ack = false;
			c.batch = false;
		},
		methods: ['DequeueWithAckId() (any, bool, string)', 'Acknowledge(ackID string) bool'],
		adds: 'The interface that makes a queue persistent. The storage hands out a job with an acknowledgement ID and learns when the job is done.',
		worker: [
			'DequeueWithAckId: once for each job it takes, when a slot is free and Len is above 0.',
			'Acknowledge: once for each job, with its ID, when the function has returned and the worker closes the job. An empty ID means nothing to acknowledge.',
			'What happens to a job that is taken and not acknowledged is the storage’s decision. So the delivery guarantee is the adapter’s.',
		],
		by: ['redisq', 'sqliteq'],
	},
	{
		id: 'ISubscribable',
		col: 2,
		row: 1,
		kind: 'block',
		on: (c) => c.sub,
		enable: (c) => {
			c.sub = true;
		},
		disable: (c) => {
			c.sub = false;
		},
		methods: ['Subscribe(func(action string))'],
		adds: 'What makes a queue distributed. The storage can call the worker back when something happens.',
		worker: [
			'WithDistributedQueue calls Subscribe once, while binding, and passes the worker’s handler.',
			'The worker acts on one action: "enqueued" wakes it to pull. Any other action is ignored.',
			'There is no way to unsubscribe. varmq’s handler does not block, so the adapter can call it from any goroutine.',
		],
		by: ['redisq'],
	},
	{
		id: 'IPersistentQueue',
		parts: 'IQueue + IAcknowledgeable',
		col: 0,
		row: 1,
		kind: 'composite',
		on: (c) => c.fifo && c.ack,
		enable: (c) => {
			useFifo(c);
			c.ack = true;
		},
		methods: [],
		adds: 'IQueue plus IAcknowledgeable. No methods of its own.',
		worker: [
			'Binding wraps it so that Add writes JSON and the worker reads with DequeueWithAckId, or with DequeueBatchWithAckIds when the storage has it.',
			'The worker takes one job per free slot. Pause and Stop do not park a job it already took.',
		],
		bind: 'WithPersistentQueue',
		by: ['redisq', 'sqliteq'],
	},
	{
		id: 'IPersistentPriorityQueue',
		parts: 'IPriorityQueue + IAcknowledgeable',
		col: 1,
		row: 1,
		kind: 'composite',
		on: (c) => c.prio && c.ack,
		enable: (c) => {
			usePrio(c);
			c.ack = true;
		},
		methods: [],
		adds: 'IPriorityQueue plus IAcknowledgeable. No methods of its own.',
		worker: ['The same as IPersistentQueue, with the priority passed to Add.'],
		bind: 'WithPersistentPriorityQueue',
		by: ['redisq', 'sqliteq'],
	},
	{
		id: 'IDistributedQueue',
		parts: 'IPersistentQueue + ISubscribable',
		col: 0,
		row: 2,
		kind: 'composite',
		on: (c) => c.fifo && c.ack && c.sub,
		enable: (c) => {
			useFifo(c);
			c.ack = true;
			c.sub = true;
		},
		methods: [],
		adds: 'IPersistentQueue plus ISubscribable. No methods of its own.',
		worker: [
			'A distributed queue lets several processes consume one queue. Each job goes to one of them.',
			'NewDistributedQueue wraps the same storage with no worker, for a process that only adds jobs.',
		],
		bind: 'WithDistributedQueue',
		by: ['redisq'],
	},
	{
		id: 'IDistributedPriorityQueue',
		parts: 'IPersistentPriorityQueue + ISubscribable',
		col: 1,
		row: 2,
		kind: 'composite',
		on: (c) => c.prio && c.ack && c.sub,
		enable: (c) => {
			usePrio(c);
			c.ack = true;
			c.sub = true;
		},
		methods: [],
		adds: 'IPersistentPriorityQueue plus ISubscribable. No methods of its own.',
		worker: ['The same as IDistributedQueue, with the priority passed to Add.'],
		bind: 'WithDistributedPriorityQueue',
		by: ['redisq'],
	},
	{
		id: 'IDedupQueue',
		col: 0,
		row: 3,
		kind: 'option',
		on: (c) => c.fifo && c.dedup,
		enable: (c) => {
			useFifo(c);
			c.dedup = true;
		},
		disable: (c) => {
			c.dedup = false;
		},
		methods: ['EnqueueOnce(item any, id string) bool'],
		adds: 'Optional. Stores the item unless an item with the same ID is already stored.',
		worker: [
			'Add calls it, in place of Enqueue, for a job whose ID is not empty.',
			'On persistent or distributed storage every ID counts. Such storage keeps an ID for a window it sets, and returns true for a repeat. A retried Add stores the job once.',
		],
		by: ['redisq', 'sqliteq'],
	},
	{
		id: 'IDedupPriorityQueue',
		col: 1,
		row: 3,
		kind: 'option',
		on: (c) => c.prio && c.dedup,
		enable: (c) => {
			usePrio(c);
			c.dedup = true;
		},
		disable: (c) => {
			c.dedup = false;
		},
		methods: ['EnqueueOnce(item any, priority int, id string) bool'],
		adds: 'Optional. IDedupQueue for a priority queue.',
		worker: ['Add calls it, in place of Enqueue, for a job whose ID is not empty.'],
		by: ['redisq', 'sqliteq'],
	},
	{
		id: 'IBatchAcknowledgeable',
		parts: 'IAcknowledgeable + 1 method',
		col: 2,
		row: 2,
		kind: 'option',
		on: (c) => c.ack && c.batch,
		enable: (c) => {
			c.ack = true;
			c.batch = true;
		},
		disable: (c) => {
			c.batch = false;
		},
		methods: ['DequeueBatchWithAckIds(items []any, ackIDs []string) (n, remaining int, err error)'],
		adds: 'Optional addition to IAcknowledgeable, for storage that can take many jobs in one call.',
		worker: [
			'When the storage is the worker’s only source, one call per wake-up takes as many jobs as there are free slots, at most 64. The worker does not call Len between jobs.',
			'Bound as a persistent queue, while it is the active worker’s only source and the worker last found it empty, the worker checks Len once a second. That is how it finds jobs another process added.',
			'Every job a call took starts, even if a pause or a stop arrives. The jobs are unacknowledged, and holding them could let a visibility timeout hand them to another consumer.',
			'Binding it signals the worker, because the storage may already hold jobs and no Add will announce them.',
		],
		by: ['redisq', 'sqliteq'],
	},
	{
		id: 'CapacitySetter',
		col: 2,
		row: 3,
		kind: 'option',
		on: (c) => c.cap,
		enable: (c) => {
			c.cap = true;
		},
		disable: (c) => {
			c.cap = false;
		},
		methods: ['SetCapacity(cap int)'],
		adds: 'Optional. Lets WithQueueCapacity reach the storage.',
		worker: [
			'varmq calls it when it creates a queue handle, with the number from WithQueueCapacity. It calls it with 0 when you passed none, so read a number below 1 as no limit.',
			'After that it is up to the storage to refuse Enqueue when it is full.',
		],
		by: [],
	},
];

/** Binding calls and what a type needs to be accepted. */
export const BINDINGS: { name: string; needs: string[]; test: (c: Caps) => boolean; get: string }[] = [
	{ name: 'WithQueue', needs: ['IQueue'], test: (c) => c.fifo, get: 'Queue[T]' },
	{ name: 'WithPriorityQueue', needs: ['IPriorityQueue'], test: (c) => c.prio, get: 'PriorityQueue[T]' },
	{ name: 'WithPersistentQueue', needs: ['IQueue', 'IAcknowledgeable'], test: (c) => c.fifo && c.ack, get: 'PersistentQueue[T]' },
	{ name: 'WithPersistentPriorityQueue', needs: ['IPriorityQueue', 'IAcknowledgeable'], test: (c) => c.prio && c.ack, get: 'PersistentPriorityQueue[T]' },
	{ name: 'WithDistributedQueue', needs: ['IQueue', 'IAcknowledgeable', 'ISubscribable'], test: (c) => c.fifo && c.ack && c.sub, get: 'DistributedQueue[T]' },
	{ name: 'WithDistributedPriorityQueue', needs: ['IPriorityQueue', 'IAcknowledgeable', 'ISubscribable'], test: (c) => c.prio && c.ack && c.sub, get: 'DistributedPriorityQueue[T]' },
	{ name: 'NewDistributedQueue', needs: ['IQueue', 'IAcknowledgeable', 'ISubscribable'], test: (c) => c.fifo && c.ack && c.sub, get: 'DistributedQueue[T], no worker' },
	{ name: 'NewDistributedPriorityQueue', needs: ['IPriorityQueue', 'IAcknowledgeable', 'ISubscribable'], test: (c) => c.prio && c.ack && c.sub, get: 'DistributedPriorityQueue[T], no worker' },
];

const has = (c: Caps, k: keyof Caps) => c[k];

/** What varmq does with a type that has these parts. */
export function behaviours(c: Caps): { label: string; text: string; on: boolean }[] {
	const enqueue = c.fifo || c.prio;
	return [
		{
			label: 'Order',
			on: enqueue,
			text: !enqueue
				? 'There is no Enqueue yet, so a job cannot get in.'
				: c.prio
					? 'Lowest number first, but only if your storage hands jobs out that way. varmq does not order the queue.'
					: 'First in, first out, as your storage hands jobs out.',
		},
		{
			label: 'Job ID',
			on: enqueue && has(c, 'dedup'),
			text:
				enqueue && c.dedup
					? 'Add calls EnqueueOnce for a job with an ID. A retried Add stores the job once.'
					: c.ack
						? 'The ID goes into the stored JSON and nothing checks it. varmq does not deduplicate for you.'
						: 'The job keeps its ID, and nothing checks it. varmq does not deduplicate for you.',
		},
		{
			label: 'Taking a job',
			on: c.ack,
			text: !c.ack
				? 'The worker calls Dequeue once for each job. Nothing is acknowledged.'
				: c.batch
					? 'One DequeueBatchWithAckIds call per wake-up, up to 64 jobs, when this storage is the worker’s only source. Closing a job calls Acknowledge.'
					: 'One job per free slot with DequeueWithAckId. Closing a job calls Acknowledge with its ID.',
		},
		{
			label: 'On bind',
			on: c.ack && c.batch,
			text: !c.ack
				? 'Nothing to say: there are no stored jobs to find.'
				: c.batch
					? 'Binding signals the worker, so it finds jobs left from an earlier run. The order of Start and bind does not matter.'
					: 'Nothing signals the worker. Create it with WithAutoRun(false), bind the queue, then call Start.',
		},
		{
			label: 'Wake-ups',
			on: c.sub || (c.ack && c.batch),
			text: c.sub
				? 'WithDistributedQueue calls Subscribe once. The adapter’s "enqueued" wakes the worker to pull.'
				: c.ack && c.batch
					? 'The worker learns of jobs from its own process: an Add, a start or resume, or one of its jobs finishing. While it is active, this storage is its only source and it last found it empty, it also checks Len once a second, so it finds jobs another process added.'
					: c.ack
						? 'The worker learns of jobs from its own process only: an Add, a start or resume, or one of its jobs finishing. A job another process adds waits until one of those happens.'
						: 'The worker looks whenever its own process signals it.',
		},
		{
			label: 'Capacity',
			on: c.cap,
			text: c.cap
				? 'SetCapacity gets the WithQueueCapacity number. Your storage has to refuse Enqueue when it is full.'
				: 'WithQueueCapacity does not make Add refuse jobs. IsFull only compares Len with the number.',
		},
	];
}

export const PRESETS: { key: string; label: string; caps: Caps }[] = [
	{ key: 'redis-p', label: 'redisq persistent', caps: { ...NO_CAPS, fifo: true, ack: true, batch: true, dedup: true } },
	{ key: 'redis-d', label: 'redisq distributed', caps: { ...NO_CAPS, fifo: true, ack: true, sub: true, batch: true, dedup: true } },
	{ key: 'sqlite', label: 'sqliteq', caps: { ...NO_CAPS, fifo: true, ack: true, batch: true, dedup: true } },
	{ key: 'clear', label: 'Clear', caps: { ...NO_CAPS } },
];
