// The home page's story: one step per chapter of the map (chapters.ts), each with the varmq code behind it, which the
// page's one code block morphs into as the map zooms to that chapter. `lines[i]` lists the 1-based lines chapter
// caption i is about, so the code lights up in step with the map. The excerpts follow the runnable programs in
// docs/examples and the SQLite and Redis pages, with the numbers the map shows. `code` fits 56 columns (a tab is 4);
// where it is wider than 40 (a tab is 2), the width of the code on a phone, `phone` is the same code broken to fit.
// stations.test.ts checks the widths, the line lists, and each varmq, sqliteq and redisq name.

export interface Station {
	code: string;
	lines: number[][];
	phone?: { code: string; lines: number[][] };
	/** Where the code changes within the step: the code from each later caption on, `then[i]` from caption i + 1,
	 * with its phone version where the station has one. It keeps the station's lines, so `lines` holds for it. */
	then?: { code: string; phone?: string }[];
	/** The full program the excerpt comes from. */
	source: { label: string; href: string };
}

const EXAMPLES = 'https://github.com/goptics/varmq/tree/main/docs/examples';

// Chapter 5 runs one burst per strategy, and its code names each in turn in the one line that sets it. No queue is
// bound with a priority, so they keep the order they are bound in, and Priority, the default, serves them by it:
// while it plays, the comments rank them. A phone has no room for them beside the bindings, so they share a line
// above them, which the other strategies leave blank.
const strategy = (name: string, phone = false) => {
	const ranked = name === 'Priority';
	const bindings = phone
		? `${ranked ? '// high, medium, low' : ''}
alerts := worker.BindQueue()
reports := worker.BindPriorityQueue()
emails := worker.BindQueue()`
		: `alerts := worker.BindQueue()${ranked ? '          // high' : ''}
reports := worker.BindPriorityQueue()${ranked ? ' // medium' : ''}
emails := worker.BindQueue()${ranked ? '          // low' : ''}`;
	return `worker := varmq.NewWorker(
	process,
	varmq.WithStrategy(varmq.${name}),
)
${bindings}`;
};

export const STATIONS: Station[] = [
	{
		code: `worker := varmq.NewWorker(func(j varmq.Job[int]) {
	fmt.Println("processing", j.Data())
})
alerts := worker.BindQueue()

alerts.Add(1)
alerts.Add(2)
alerts.Add(3)`,
		lines: [[6, 7, 8]],
		phone: {
			code: `worker := varmq.NewWorker(
	func(j varmq.Job[int]) {
		fmt.Println(j.Data())
	},
)
alerts := worker.BindQueue()

alerts.Add(1)
alerts.Add(2)
alerts.Add(3)`,
			lines: [[8, 9, 10]],
		},
		source: { label: 'quick-start', href: `${EXAMPLES}/quick-start` },
	},
	{
		code: `// one goroutine to start
worker := varmq.NewWorker(process)
alerts := worker.BindQueue()

worker.TunePool(5)
for i := range 5 {
	alerts.Add(i)
}`,
		lines: [
			[5, 6, 7, 8],
			[2, 5],
		],
		source: { label: 'concurrency-tunepool', href: `${EXAMPLES}/concurrency-tunepool` },
	},
	{
		code: `// atoi parses the job's string
worker := varmq.NewResultWorker(atoi)
queue := worker.BindQueue()

job, _ := queue.Add("42")
n, err := job.Result() // 42 <nil>

job, _ = queue.Add("forty-two")
n, err = job.Result() // 0, an error`,
		lines: [[6, 9]],
		source: { label: 'worker-types-result', href: `${EXAMPLES}/worker-types-result` },
	},
	{
		code: `reports := worker.BindPriorityQueue()

reports.Add("five", 5)
reports.Add("two", 2)
reports.Add("eight", 8)
reports.Add("one", 1)
reports.Add("three", 3)
// they run 1, 2, 3, 5, 8`,
		lines: [[6, 8]],
		source: { label: 'queues-priority', href: `${EXAMPLES}/queues-priority` },
	},
	{
		code: strategy('Priority'),
		lines: [[3, 5, 6, 7], [3], [3], [3]],
		phone: { code: strategy('Priority', true), lines: [[3, 5, 6, 7, 8], [3], [3], [3]] },
		then: ['MaxLen', 'MinLen', 'RoundRobin'].map((name) => ({ code: strategy(name), phone: strategy(name, true) })),
		source: { label: 'the strategies guide', href: 'https://varmq.goptics.org/guides/strategies/' },
	},
	{
		code: `topic := varmq.NewTopic[int]()

worker.WithTopic(topic)
another.WithTopic(topic)

topic.Publish(1) // a job for each
topic.Publish(2)`,
		lines: [[6, 7]],
		source: { label: 'topic-subscribe', href: `${EXAMPLES}/topic-subscribe` },
	},
	{
		code: `sdb, _ := sqliteq.New("data")
pq, _ := sdb.NewQueue("jobs")

worker := varmq.NewWorker(process, 2)
queue := worker.WithPersistentQueue(pq)

queue.Add("job 1")
queue.Add("job 2")`,
		lines: [[1, 2, 5], [7, 8], [4], [], [1, 2, 5], [4]],
		source: { label: 'the SQLite page', href: 'https://varmq.goptics.org/persistence/sqlite/' },
	},
	{
		code: `rdb := redisq.New("redis://localhost:6379")
dq := rdb.NewDistributedQueue("jobs")

// program A adds
producer := varmq.NewDistributedQueue[string](dq)
producer.Add("job 1")

// programs B and C run the jobs
worker := varmq.NewWorker(process, 2)
worker.WithDistributedQueue(dq)`,
		lines: [[1, 2], [5, 6], [], [10], [9, 10]],
		phone: {
			code: `rdb := redisq.New(
	"redis://localhost:6379",
)
dq := rdb.NewDistributedQueue("jobs")

// program A adds
q := varmq.NewDistributedQueue[string](
	dq,
)
q.Add("job 1")

// programs B and C run the jobs
worker := varmq.NewWorker(process, 2)
worker.WithDistributedQueue(dq)`,
			lines: [[1, 2, 3, 4], [7, 8, 9, 10], [], [14], [13, 14]],
		},
		source: { label: 'the Redis page', href: 'https://varmq.goptics.org/persistence/redis/' },
	},
	{
		code: `worker.Pause()
worker.Resume()

// running jobs finish, then the event
// loop and its goroutines exit
worker.Stop()
// the waiting jobs run
worker.Restart()`,
		lines: [[1], [2], [4, 5, 6], [7, 8]],
		source: { label: 'lifecycle-restart', href: `${EXAMPLES}/lifecycle-restart` },
	},
	{
		code: `worker := varmq.NewWorker(process)

worker.BindQueue()              // memory
worker.WithPersistentQueue(pq)  // SQLite
worker.WithDistributedQueue(dq) // Redis`,
		lines: [[1, 3, 4, 5]],
		phone: {
			code: `worker := varmq.NewWorker(process)

// memory
worker.BindQueue()
// SQLite
worker.WithPersistentQueue(pq)
// Redis
worker.WithDistributedQueue(dq)`,
			lines: [[1, 3, 4, 5, 6, 7, 8]],
		},
		source: { label: 'workers-and-queues', href: `${EXAMPLES}/workers-and-queues` },
	},
];
