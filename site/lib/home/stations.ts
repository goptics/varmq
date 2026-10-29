// The home page's story: one station per chapter of the map (chapters.ts), each with the varmq code behind it.
// `lines[i]` lists the 1-based lines that chapter's caption i is about, so the code lights up in step with the map.
// The excerpts follow the runnable programs in docs/examples and the SQLite and Redis pages, with the numbers the
// map shows; stations.test.ts checks each varmq, sqliteq and redisq name in them against the package and the docs.

export interface Station {
	code: string;
	lines: number[][];
	/** The full program the excerpt comes from. */
	source: { label: string; href: string };
}

const EXAMPLES = 'https://github.com/goptics/varmq/tree/main/docs/examples';

export const STATIONS: Station[] = [
	{
		code: `worker := varmq.NewWorker(func(j varmq.Job[int]) {
	fmt.Println("processing", j.Data())
})
queue := worker.BindQueue()

queue.Add(1)
queue.Add(2)
queue.Add(3)`,
		lines: [[6, 7, 8]],
		source: { label: 'quick-start', href: `${EXAMPLES}/quick-start` },
	},
	{
		code: `worker := varmq.NewWorker(process) // one goroutine
queue := worker.BindQueue()

worker.TunePool(5)
for i := range 5 {
	queue.Add(i)
}`,
		lines: [
			[4, 5, 6, 7],
			[1, 4],
		],
		source: { label: 'concurrency-tunepool', href: `${EXAMPLES}/concurrency-tunepool` },
	},
	{
		code: `worker := varmq.NewResultWorker(func(j varmq.Job[string]) (int, error) {
	return strconv.Atoi(j.Data())
})
queue := worker.BindQueue()

for _, s := range []string{"42", "forty-two"} {
	job, _ := queue.Add(s)
	n, err := job.Result() // 42 <nil>, then 0 and an error
}`,
		lines: [[2, 7, 8]],
		source: { label: 'worker-types-result', href: `${EXAMPLES}/worker-types-result` },
	},
	{
		code: `queue := worker.BindPriorityQueue()

for _, p := range []int{5, 2, 8, 1, 3} {
	queue.Add(fmt.Sprint("job ", p), p)
}
// taken in the order 1, 2, 3, 5, 8`,
		lines: [[4, 6]],
		source: { label: 'queues-priority', href: `${EXAMPLES}/queues-priority` },
	},
	{
		code: `worker := varmq.NewWorker(process,
	varmq.WithStrategy(varmq.RoundRobin),
)
queue := worker.BindQueue()
urgent := worker.BindPriorityQueue()
batch := worker.BindQueue()`,
		lines: [[2, 4, 6]],
		source: { label: 'strategies-round-robin', href: `${EXAMPLES}/strategies-round-robin` },
	},
	{
		code: `topic := varmq.NewTopic[int]()

worker.WithTopic(topic)
another.WithTopic(topic)

topic.Publish(1) // each worker gets its own job
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
		source: { label: 'the Redis page', href: 'https://varmq.goptics.org/persistence/redis/' },
	},
	{
		code: `worker.Pause()
worker.Resume()

worker.Stop()    // running jobs finish, waiting ones stay
worker.Restart() // and the waiting jobs run`,
		lines: [[1, 2], [4], [5]],
		source: { label: 'lifecycle-restart', href: `${EXAMPLES}/lifecycle-restart` },
	},
	{
		code: `worker := varmq.NewWorker(process)

worker.BindQueue()              // memory
worker.WithPersistentQueue(pq)  // SQLite
worker.WithDistributedQueue(dq) // Redis`,
		lines: [[1, 3, 4, 5]],
		source: { label: 'workers-and-queues', href: `${EXAMPLES}/workers-and-queues` },
	},
];
