// The setup steps of the Redis and SQLite pages. The commands and programs are the ones the pages already
// ran: examples/redis-persistent, examples/redis-distributed and examples/sqlite-persistent.

export interface SetupCode {
	label?: string;
	/** A storage: the tab shows its mark and name (ui/stores.ts). */
	store?: 'memory' | 'sqlite' | 'redis';
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
		text: `<a href="${pkg}/redisq#New"><code>redisq.New</code></a> takes a Redis URL and returns a <a href="${pkg}/redisq#Queues"><code>Queues</code></a> value. It panics when the URL does not parse. Each queue takes a key, which names it in Redis. Pick the order.`,
		code: [
			{
				label: 'FIFO',
				lang: 'go',
				file: 'main.go',
				code:
					'rdb := redisq.New("redis://localhost:6375")\ndefer rdb.Close()\n\n' +
					'rq := rdb.NewQueue("jobs")\ndefer rq.Close()\n\n' +
					'queue := worker.WithQueue(rq)',
				note: 'First in, first out, on a Redis list. Jobs survive a restart. Several processes can consume one queue, and each job goes to one of them.',
			},
			{
				label: 'Priority',
				lang: 'go',
				file: 'main.go',
				code:
					'rdb := redisq.New("redis://localhost:6375")\ndefer rdb.Close()\n\n' +
					'rq := rdb.NewPriorityQueue("priority_jobs")\ndefer rq.Close()\n\n' +
					'queue := worker.WithPriorityQueue(rq)\nqueue.Add("alert", 1) // a lower number runs first',
				note: 'Lowest number first, on a sorted set.',
			},
			{
				label: 'Producer only',
				lang: 'go',
				file: 'main.go',
				code:
					'rdb := redisq.New("redis://localhost:6375")\ndefer rdb.Close()\n\n' +
					'rq := rdb.NewQueue("jobs")\ndefer rq.Close()\n\n' +
					'producer := varmq.NewQueue[string](rq)\njob, _ := producer.Add("job 1")',
				note: 'A process that only adds jobs. Adding a job wakes an idle consumer in another process.',
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
				label: 'FIFO',
				lang: 'go',
				file: 'main.go',
				code: 'pq, err := sdb.NewQueue("jobs")\nif err != nil {\n\tpanic(err)\n}\ndefer pq.Close()\n\nqueue := worker.WithQueue(pq)',
			},
			{
				label: 'Priority',
				lang: 'go',
				file: 'main.go',
				code: 'pq, err := sdb.NewPriorityQueue("priority_jobs")\nif err != nil {\n\tpanic(err)\n}\ndefer pq.Close()\n\nqueue := worker.WithPriorityQueue(pq)\nqueue.Add("alert", 1) // a lower number runs first',
			},
		],
	},
];

// The first workflow of guides/workflows.mdx: download, resize, save. examples/workflows-chain is the in-memory
// program; the SQLite and Redis tabs ran against sqliteq and redisq with the same three tasks.
const worker = `steps := varmq.NewErrWorker(func(j varmq.Job[string]) error {
	fmt.Println("run:", j.Data())
	return nil
})`;

export const workflowSteps: SetupStep[] = [
	{
		title: 'Create the workflow',
		text: 'The workflow needs a store, which keeps its state. Pick one.',
		code: [
			{
				store: 'memory',
				lang: 'go',
				code: 'wf := varmq.NewWorkflow(varmq.NewWorkflowStore())',
				note: `<a href="${pkg}/varmq#NewWorkflowStore"><code>NewWorkflowStore</code></a> keeps the state in your process. It is lost when the process exits.`,
			},
			{
				store: 'sqlite',
				lang: 'go',
				code:
					'qs, err := sqliteq.New("data")\nif err != nil {\n\tpanic(err)\n}\ndefer qs.Close()\n\n' +
					'store, err := sqliteq.NewWorkflowStore(qs)\nif err != nil {\n\tpanic(err)\n}\ndefer store.Close()\n\n' +
					'wf := varmq.NewWorkflow(store)',
				note: `<a href="${pkg}/sqliteq#NewWorkflowStore"><code>sqliteq.NewWorkflowStore</code></a> keeps the state in SQLite, so the workflow continues after a restart. The <code>defer</code> order closes the store before the client.`,
			},
			{
				store: 'redis',
				lang: 'go',
				code:
					'qs := redisq.New("redis://localhost:6375")\ndefer qs.Close()\n\n' +
					'store, err := redisq.NewWorkflowStore(qs)\nif err != nil {\n\tpanic(err)\n}\ndefer store.Close()\n\n' +
					'wf := varmq.NewWorkflow(store)',
				note: `<a href="${pkg}/redisq#NewWorkflowStore"><code>redisq.NewWorkflowStore</code></a> keeps the state in Redis, so the workflow continues after a restart and other processes can run its tasks. Port 6375 is the Redis of <a href="/persistence/redis/#setup">Redis setup</a>.`,
			},
		],
	},
	{
		title: 'Create a worker',
		text: 'Each task is one job, so a worker must run it. Here one worker runs all three tasks. Its queue must use the same storage as the store.',
		code: [
			{ store: 'memory', lang: 'go', code: `${worker}.BindQueue()` },
			{
				store: 'sqlite',
				lang: 'go',
				code: 'queue, err := qs.NewQueue("steps")\nif err != nil {\n\tpanic(err)\n}\n' + `${worker}.WithQueue(queue)`,
			},
			{ store: 'redis', lang: 'go', code: `${worker}.WithQueue(qs.NewQueue("steps"))` },
		],
	},
	{
		title: 'Add the first task',
		text: '<code>wf.Add(name, queue, data)</code> adds a task. Nothing runs yet. This task has no dependencies, so it starts first.',
		code: [{ lang: 'go', code: 'download := wf.Add("download", steps, "download photo.png")' }],
	},
	{
		title: 'Add a task that waits',
		text: '<code>After(download)</code> makes this task wait until <code>download</code> has ended.',
		code: [{ lang: 'go', code: 'resize := wf.Add("resize", steps, "resize to 64px").After(download)' }],
	},
	{
		title: 'Add the last task',
		text: 'It waits for <code>resize</code>. The steps from here are the same for every store.',
		code: [{ lang: 'go', code: 'wf.Add("save", steps, "save to disk").After(resize)' }],
	},
	{
		title: 'Submit and wait',
		text: `<a href="${pkg}/varmq#Workflow.Submit"><code>Submit</code></a> starts the workflow. <a href="${pkg}/varmq#Workflow.Wait"><code>Wait</code></a> blocks until it ends. <a href="${pkg}/varmq#Workflow.WaitContext"><code>WaitContext</code></a> does the same with a deadline.`,
		code: [
			{
				lang: 'go',
				code: 'if err := wf.Submit(context.Background()); err != nil {\n\tpanic(err)\n}\nrec, err := wf.Wait()\nif err != nil {\n\tpanic(err)\n}\nfmt.Println("workflow:", rec.State)',
				note: 'It prints <code>run: download photo.png</code>, <code>run: resize to 64px</code>, <code>run: save to disk</code>, then <code>workflow: completed</code>.',
			},
		],
	},
];
