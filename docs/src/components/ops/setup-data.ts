// The setup steps of the Redis and SQLite pages. The commands and programs are the ones the pages already
// ran: examples/redis-persistent, examples/redis-distributed and examples/sqlite-persistent.

export interface SetupCode {
	label?: string;
	file?: string;
	lang?: 'bash' | 'go';
	code: string;
	note?: string;
}
export interface SetupStep {
	title: string;
	text?: string;
	code: SetupCode[];
}

const pkg = 'https://pkg.go.dev/github.com/goptics';

export const redisSetup: SetupStep[] = [
	{
		title: 'Start Redis',
		text: 'varmq’s repository has <code>examples/compose.yml</code>, which starts Redis 7.2 on port 6375. The programs on this page use that port.',
		code: [
			{
				label: 'From a clone',
				lang: 'bash',
				code: 'docker compose -f examples/compose.yml up -d',
				note: 'Run it from a clone of <a href="https://github.com/goptics/varmq">github.com/goptics/varmq</a>.',
			},
			{
				label: 'Without a clone',
				lang: 'bash',
				code: 'docker run -d --name varmq-redis -p 6375:6379 redis:7.2-alpine',
				note: 'Without a clone, the same image does the job.',
			},
		],
	},
	{
		title: 'Add the two modules',
		text: 'varmq’s version is the release these pages describe.',
		code: [{ lang: 'bash', code: 'go get github.com/goptics/varmq github.com/goptics/redisq' }],
	},
	{
		title: 'Create a queue and bind it',
		text: `<a href="${pkg}/redisq#New"><code>redisq.New</code></a> takes a Redis URL and returns a <a href="${pkg}/redisq#Queues"><code>Queues</code></a> value. It panics when the URL does not parse. Each queue takes a key, which names it in Redis. Pick the kind of queue.`,
		code: [
			{
				label: 'Persistent',
				lang: 'go',
				file: 'main.go',
				code:
					'rdb := redisq.New("redis://localhost:6375")\ndefer rdb.Close()\n\n' +
					'pq := rdb.NewQueue("persistent_queue")\ndefer pq.Close()\n\n' +
					'queue := worker.WithPersistentQueue(pq)',
				note: 'First in, first out, on a Redis list. One process keeps its jobs across a restart.',
			},
			{
				label: 'Persistent priority',
				lang: 'go',
				file: 'main.go',
				code:
					'rdb := redisq.New("redis://localhost:6375")\ndefer rdb.Close()\n\n' +
					'pq := rdb.NewPriorityQueue("persistent_priority_queue")\ndefer pq.Close()\n\n' +
					'queue := worker.WithPersistentPriorityQueue(pq)\nqueue.Add("alert", 1) // a lower number runs first',
				note: 'Lowest number first, on a sorted set.',
			},
			{
				label: 'Distributed',
				lang: 'go',
				file: 'main.go',
				code:
					'rdb := redisq.New("redis://localhost:6375")\ndefer rdb.Close()\n\n' +
					'dq := rdb.NewDistributedQueue("distributed_queue")\ndefer dq.Close()\n\n' +
					'queue := worker.WithDistributedQueue(dq)\n\n' +
					'// or, in a process that only adds jobs:\n// producer := varmq.NewDistributedQueue[string](dq)',
				note: 'Several processes consume one queue, and each job goes to one of them. Adding a job wakes an idle consumer.',
			},
			{
				label: 'Distributed priority',
				lang: 'go',
				file: 'main.go',
				code:
					'rdb := redisq.New("redis://localhost:6375")\ndefer rdb.Close()\n\n' +
					'dq := rdb.NewDistributedPriorityQueue("distributed_priority_queue")\ndefer dq.Close()\n\n' +
					'queue := worker.WithDistributedPriorityQueue(dq)',
				note: 'Lowest number first, plus wake-ups for idle consumers.',
			},
		],
	},
];

export const sqliteSetup: SetupStep[] = [
	{
		title: 'Add the two modules',
		text: 'sqliteq uses <a href="https://github.com/mattn/go-sqlite3">mattn/go-sqlite3</a>, which is built with cgo, so building needs <code>CGO_ENABLED=1</code> and a C compiler.',
		code: [{ lang: 'bash', code: 'go get github.com/goptics/varmq github.com/goptics/sqliteq' }],
	},
	{
		title: 'Open the store',
		text: `<a href="${pkg}/sqliteq#New"><code>sqliteq.New</code></a> takes the path of a folder and returns a <a href="${pkg}/sqliteq#Queues"><code>Queues</code></a> value and an error. The folder is created if it does not exist.`,
		code: [
			{
				lang: 'go',
				file: 'main.go',
				code: 'sdb, err := sqliteq.New("data")\nif err != nil {\n\tpanic(err)\n}\ndefer sdb.Close()',
			},
		],
	},
	{
		title: 'Create a queue and bind it',
		text: 'Each queue gets its own database file in the folder, named after the queue in lower case: the queue <code>jobs</code> lives in <code>jobs.db</code>. Both constructors take the name of the queue and return an error as well as the queue.',
		code: [
			{
				label: 'First in, first out',
				lang: 'go',
				file: 'main.go',
				code: 'pq, err := sdb.NewQueue("jobs")\nif err != nil {\n\tpanic(err)\n}\ndefer pq.Close()\n\nqueue := worker.WithPersistentQueue(pq)',
			},
			{
				label: 'Lowest number first',
				lang: 'go',
				file: 'main.go',
				code: 'pq, err := sdb.NewPriorityQueue("priority_jobs")\nif err != nil {\n\tpanic(err)\n}\ndefer pq.Close()\n\nqueue := worker.WithPersistentPriorityQueue(pq)\nqueue.Add("alert", 1) // a lower number runs first',
			},
		],
	},
];
