<div align="center">
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/text-logo-dark.svg">
  
  <source media="(prefers-color-scheme: light)" srcset="./assets/text-logo.svg">
  
  <img alt="VarMQ" width="400px" src="./assets/text-logo.svg">
</picture>


  <p>
    <a href="https://github.com/avelino/awesome-go?tab=readme-ov-file#messaging"><img src="https://awesome.re/mentioned-badge-flat.svg" alt="Mentioned in Awesome Go" /></a>
    <a href="https://pkg.go.dev/github.com/goptics/varmq"><img src="https://img.shields.io/badge/go-pkg-00ADD8.svg?logo=go" alt="Go Reference" /></a>
    <a href="https://github.com/goptics/varmq/actions/workflows/ci.yml"><img src="https://github.com/goptics/varmq/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
    <a href="https://codecov.io/gh/goptics/varmq"><img src="https://codecov.io/gh/goptics/varmq/branch/main/graph/badge.svg" alt="Codecov" /></a>
    <a href="https://golang.org/doc/devel/release.html"><img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go" alt="Go Version" /></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square" alt="License" /></a>
  </p>

  <p align="center">
    <a href="#features">Features</a> ·
    <a href="#quick-start">Quick Start</a> ·
    <a href="#usage">Usage</a> ·
    <a href="#how-it-works">How It Works</a> ·
    <a href="#comparison-and-benchmarks">Comparison</a> ·
    <a href="#api-reference">API Reference</a> ·
    <a href="#contributing">Contributing</a>
  </p>

  <p>
    A typed worker pool and message queue for Go. Each worker is a concurrent pool that pulls jobs from any mix of in-memory, priority, <a href="#persistent-and-distributed-queues">persistent or distributed</a> queues and <a href="#topic-fan-out">topics</a>.
  </p>
</div>

## Features

- **High performance**: lock-free appends and [batched dispatch](#batched-dispatch). [See benchmarks](#performance).
- **Queue types**: standard, priority, persistent (survives restarts) and distributed (shared across instances).
- **Worker types**: `NewWorker` (fire and forget), `NewErrWorker` (error only), `NewResultWorker` (result and error).
- **Concurrency control**: resize the pool at runtime, pause, resume, restart.
- **Multi queue binding**: one worker, many queues, a pluggable selection strategy.
- **Topic fan-out**: publish once, every subscribed worker gets its own job.
- **REST API**: inspect and control workers over HTTP.
- **Extensible**: plug in your own storage by implementing [`IPersistentQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPersistentQueue) or [`IDistributedQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDistributedQueue).

## Quick Start

```bash
go get github.com/goptics/varmq
```

```go
package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Printf("Processing %d\n", j.Data())
		time.Sleep(1 * time.Second)
	}) // default concurrency is 1
	defer worker.WaitUntilIdle()
	queue := worker.BindQueue()

	queue.Add(1)
	queue.Add(2)
}
```

↗️ **[Run it on Playground](https://go.dev/play/p/mobZd9pKcug)**

## Usage

Runnable programs for most sections live in [examples](./examples).

### Concurrency

```go
worker := varmq.NewWorker(func(j varmq.Job[int]) {
	fmt.Printf("Processing %d\n", j.Data())
	time.Sleep(500 * time.Millisecond)
}, 10) // concurrency 10, or 0 for one per CPU
defer worker.WaitUntilIdle()
queue := worker.BindQueue()

for i := range 100 {
	queue.Add(i)
}
```

↗️ **[Run it on Playground](https://go.dev/play/p/uP0rA-NrzZB)**

> [!Note]
> Concurrency is a limit, not a fixed pool size: goroutines start while jobs wait and exit as the queue drains. Tune it with `varmq.WithMinIdleWorkerRatio` and `varmq.WithIdleWorkerExpiryDuration`.

### Priority Queue

Lower number = higher priority. A higher-priority job added later is dispatched first.

```go
queue := worker.BindPriorityQueue()

for i := range 10 {
	queue.Add(i, i%2) // even jobs first
}
```

↗️ **[Run it on Playground](https://go.dev/play/p/CHF8PVyrBI0)**

![VarMQ priority queue with one worker](./assets/diagrams/varmq-priority-queue.svg)

### Deduplicate by Job ID

An in-memory queue holds at most one waiting job per id. The id is free again once a worker takes the job or `Purge` drops it. Jobs without an id are never deduplicated.

```go
queue.Add(userID, varmq.WithJobId("refresh:"+userID)) // queued
queue.Add(userID, varmq.WithJobId("refresh:"+userID)) // nil, false: one is already waiting
```

### Persistent and Distributed Queues

Plug in a storage adapter to keep jobs across restarts or share them between instances:

```go
queue := worker.WithPersistentQueue(persistentAdapter)   // any IPersistentQueue
queue := worker.WithDistributedQueue(distributedAdapter) // any IDistributedQueue
```

Examples: [SQLite persistent](./examples/sqlite-persistent), [Redis persistent](./examples/redis-persistent), [Redis distributed](./examples/redis-distributed).

When the adapter deduplicates pushes (`IDedupQueue`, e.g. redisq), give each job a unique id such as a UUID. A retried `Add` with the same id is then stored once:

```go
id := uuid.NewString() // once, so a retry carries the same id
for !queue.Add(data, varmq.WithJobId(id)) {
	// back off and retry
}
```

#### Built-in adapters

- ⚡ Redis: [redisq](https://github.com/goptics/redisq)
- 🗃️ SQLite: [sqliteq](https://github.com/goptics/sqliteq)
- 🐘 PostgreSQL: 🔄 Upcoming

### Multi Queue Binds

Bind several queues to one worker. `varmq.WithStrategy` picks which queue the next job comes from:

1. **Priority** (default): the highest-priority queue with pending jobs
2. **RoundRobin**: take turns across queues
3. **MaxLen**: the longest queue
4. **MinLen**: the shortest queue

```go
worker := varmq.NewWorker(func(j varmq.Job[string]) {
	fmt.Println("Processing:", j.Data())
	time.Sleep(500 * time.Millisecond)
})
defer worker.WaitUntilIdle()

// queue priority follows bind order; override with varmq.WithQueuePriority
q1 := worker.BindQueue()         // highest
q2 := worker.BindQueue()         // medium
pq := worker.BindPriorityQueue() // lowest

for i := range 15 {
	q2.Add(fmt.Sprintf("Task queue-2 %d", i))
}

for i := range 10 {
	pq.Add(fmt.Sprintf("Task priority-queue %d", i), i%2)
}

for i := range 10 {
	q1.Add(fmt.Sprintf("Task queue-1 %d", i))
}
```

↗️ **[Run it on Playground](https://go.dev/play/p/0eL_0WNRVIh)**

With `varmq.RoundRobin`, empty queues are skipped:

![VarMQ multiple queues with round-robin selection](./assets/diagrams/varmq-multi-queue.svg)

### Topic (Fan-out)

One producer, many workers: publish once and every subscribed worker gets its own job. `Publish` returns `false` when no worker is subscribed, the topic is closed, or it is full.

| | Queue | Topic |
|---|---|---|
| Who gets a job | each job is processed once, by one worker | every subscribed worker gets its own job |
| Storage | the job is removed once a worker takes it | the payload is stored once; each worker reads it with its own cursor, and it's freed after every worker has read it |
| Producer | `queue.Add(x)` | `topic.Publish(x)` |
| Late joiner | sees jobs still waiting in the queue | sees only what's published after it subscribed |

```go
topic := varmq.NewTopic[int]()

email := varmq.NewWorker(func(j varmq.Job[int]) {
	fmt.Printf("Emailing order %d\n", j.Data())
	time.Sleep(1 * time.Second)
})
stock := varmq.NewWorker(func(j varmq.Job[int]) {
	fmt.Printf("Updating stock for order %d\n", j.Data())
	time.Sleep(300 * time.Millisecond)
}, 2)
defer email.WaitUntilIdle()
defer stock.WaitUntilIdle()

email.WithTopic(topic)
stock.WithTopic(topic)

for order := range 4 {
	topic.Publish(order) // both workers get all 4 orders
}
```

↗️ **[Run it on Playground](https://go.dev/play/p/NeIa69kRi5s)**

![VarMQ topic fan-out with two subscribed workers](./assets/diagrams/varmq-topic.svg)

### Result and Error Worker

`NewResultWorker` returns a result and an error per job. `NewErrWorker` returns only the error.

```go
worker := varmq.NewResultWorker(func(j varmq.Job[string]) (int, error) {
	if j.Data() == "error" {
		return 0, errors.New("error occurred")
	}
	return len(j.Data()), nil
})
defer worker.WaitUntilIdle()
queue := worker.BindQueue()

// Add returns at once; Result blocks until that job is done
if job, ok := queue.Add("The length of this string is 31"); ok {
	result, err := job.Result()
	fmt.Println("Result:", result, err) // Result: 31 <nil>
}

if job, ok := queue.Add("error"); ok {
	_, err := job.Result()
	fmt.Println("Error:", err) // Error: error occurred
}
```

↗️ **[Run it on Playground](https://go.dev/play/p/nKnAyQfkxou)**

### Function Helpers

Submit functions directly, like `Submit()` in [pond](https://github.com/alitto/pond) or [ants](https://github.com/panjf2000/ants). Use `Func()` with `NewWorker`, `ErrFunc()` with `NewErrWorker` and `ResultFunc[R]()` with `NewResultWorker`.

```go
worker := varmq.NewWorker(varmq.Func(), 10)
defer worker.WaitUntilIdle()
queue := worker.BindQueue()

for i := range 100 {
	queue.Add(func() {
		time.Sleep(500 * time.Millisecond)
		fmt.Println("Processing", i)
	})
}
```

↗️ **[Run it on Playground](https://go.dev/play/p/J2xXmVlGYyW)**

> [!Important]
> Functions can't be serialized, so function helpers don't work with persistent or distributed queues.

### Lifecycle and Errors

Every call returns at once; running jobs always finish.

```go
worker := varmq.NewWorker(process, 4,
	varmq.WithContext(ctx), // cancelling ctx stops the worker
	varmq.WithErrHandler(func(w varmq.Worker, err error) {
		log.Println("varmq:", err) // dequeue, parse and other internal errors
	}),
)

worker.Pause()     // stop taking new jobs; unstarted jobs stay queued
worker.Resume()    // carry on
worker.TunePool(8) // change concurrency on the fly
worker.Restart()   // stop and start again, keeping pending jobs
worker.Stop()      // graceful stop

worker.Wait()          // block until no job is running or pending
worker.WaitUntilIdle() // block until the worker is Idle (never returns while paused or stopped)
```

`Status()` reports the state (`Running`, `Idle`, `Paused`, `Stopped`, ...). `PauseAndWait`, `StopAndWait`, `WaitUntilPaused` and `WaitUntilStopped` block until a state is reached.

### REST API

Mount the handler on any `http.ServeMux`:

```go
mux := http.NewServeMux()
mux.Handle("/varmq/", varmq.Handler("/varmq"))
http.ListenAndServe(":8080", mux)
```

| Method   | Path                                      | Description                  |
|----------|-------------------------------------------|------------------------------|
| `GET`    | `/health`                                 | Health check                 |
| `GET`    | `/workers`                                | List all workers (summary)   |
| `GET`    | `/workers/{name}`                         | Get worker details + metrics |
| `PATCH`  | `/workers/{name}/actions/{action}`        | Pause, resume, stop, restart |
| `PATCH`  | `/workers/{name}/config/concurrency/{n}`  | Adjust concurrency           |

> [!Warning]
> The REST API is unauthenticated by design. Protect it with middleware, network isolation, or a reverse proxy before exposing to untrusted networks.

## How It Works

### Event loop

A single event loop sleeps until something signals it: `Add()` putting a job in the queue, or a worker finishing one. On each signal it hands jobs to workers while the queue has work and a worker slot is free, then goes back to waiting.

![VarMQ event loop with one worker](./assets/diagrams/varmq-single-worker.svg)

### Worker pool

Every `Add` (and every job in `AddAll`) tries to wake the event loop, but the signal channel holds only one pending signal, so the extra signals are simply dropped. One wake-up is enough: the loop keeps dispatching jobs back to back while the queue has work and a worker slot is free, and it hands each worker its share of the backlog in one go (a batch) rather than one job per wake-up.

![VarMQ worker pool with concurrency 4](./assets/diagrams/varmq-worker-pool.svg)

### Batched dispatch

A hand-off costs the event loop the same whether a job takes a nanosecond or a minute, so with many short jobs the loop, not the workers, becomes the bottleneck. To avoid that, a worker can take several jobs at once (a batch) and run them in order.

Batch size is the smallest of: an even split of the waiting jobs across workers, enough jobs to keep hand-offs about 32 µs apart (sized from the last finished batch; one job until a job has been measured), and 32. With enough jobs waiting:

| Setup | Jobs per hand-off |
|---|---|
| 10 workers, 3 s jobs | 1, so jobs start in queue order |
| 24 workers, 1 ms jobs | 1 |
| 3,000 workers, 10 ms jobs | 10 |
| 10,000+ workers, 10 ms jobs | 32 |
| Concurrency 1, any jobs | up to 32: one worker runs jobs in order anyway |

Below, 8 cheap jobs with concurrency 2: the first worker takes 4, a second worker is started for 2, and the last 2 go out one at a time as workers free up.

![VarMQ batched dispatch](./assets/diagrams/varmq-batch.svg)

- **Jobs waiting in a batch** stay `Queued`: `NumPending` counts them, `NumProcessing` does not, and you can still close them.
- **On `Stop` or `Pause`** each worker finishes its running job and parks the rest of its batch. `NumPending` counts parked jobs but the queue's `Len` does not. They run first, in order, on `Resume` or `Restart`, and `Purge` closes them.
- **Capacity** counts only jobs still in the queue, so up to 32 more per worker can be pending.

| Queue | Batches? | Why |
|---|---|---|
| In-memory queue, topic | Yes | |
| Priority queue | No | A higher-priority job added later must not wait behind a batch. |
| Persistent or distributed queue | No | A worker must not hold unacknowledged messages that other consumers could take. |
| Worker bound to several queues | No | The strategy picks a queue for every single job. |

<a id="benchmarks"></a>

## Comparison and Benchmarks

`VarMQ` is compared with [ants](https://github.com/panjf2000/ants), [pond v2](https://github.com/alitto/pond), [conc](https://github.com/sourcegraph/conc), [workerpool](https://github.com/gammazero/workerpool), [errgroup](https://pkg.go.dev/golang.org/x/sync/errgroup), [gopool](https://github.com/bytedance/gopkg/tree/main/util/gopool) and two hand-rolled channel pools in the [benchmark repository](https://github.com/goptics/varmq-benchmarks).

### Feature comparison

| Feature                                  | VarMQ | ants | pond | conc | workerpool | errgroup | gopool |
| ---------------------------------------- | :---: | :--: | :-----: | :--: | :--------: | :------: | :----: |
| Generic (type-safe) API                  |  ✅   |  ✅  |   ✅    |  ✅  |     ❌     |    ❌    |   ❌   |
| Tasks return results and errors          |  ✅   |  ❌  |   ✅    |  ✅  |     ❌     |   ❌²    |   ❌   |
| Task groups (submit many, wait once)     |  ✅   |  ❌  |   ✅    |  ✅  |     ❌     |    ✅    |   ❌   |
| Priority queue                           |  ✅   |  ❌  |   ❌    |  ❌  |     ❌     |    ❌    |   ❌   |
| Persistent queues (survive restarts)     |  ✅   |  ❌  |   ❌    |  ❌  |     ❌     |    ❌    |   ❌   |
| Distributed queues (across instances)    |  ✅   |  ❌  |   ❌    |  ❌  |     ❌     |    ❌    |   ❌   |
| Several queues bound to one pool         |  ✅   |  ❌  |   ✅⁴   |  ❌  |     ❌     |    ❌    |   ❌   |
| Topic fan-out (publish once, many pools) |  ✅   |  ❌  |   ❌    |  ❌  |     ❌     |    ❌    |   ❌   |
| Bounded queue, reject when full          |  ✅   |  ✅  |   ✅    |  ❌  |     ❌     |   ❌³    |   ❌   |
| Pause and resume                         |  ✅   |  ❌  |   ❌    |  ❌  |     ✅     |    ❌    |   ❌   |
| Restart after stop                       |  ✅   |  ✅  |   ❌    |  ❌  |     ❌     |    ❌    |   ❌   |
| Resize pool at runtime                   |  ✅   |  ✅  |   ✅    |  ❌  |     ❌     |    ❌    |   ✅   |
| Cancel work via context                  |  ✅   |  ❌  |   ✅    |  ✅  |     ❌     |    ✅    |   ❌   |
| Panic recovery                           |  ✅   |  ✅  |   ✅    | ❌¹  |     ❌     |    ❌    |   ✅   |
| Task counters (submitted, failed, ...)   |  ✅   |  ❌  |   ✅    |  ❌  |     ❌     |    ❌    |   ❌   |
| Built-in REST API                        |  ✅   |  ❌  |   ❌    |  ❌  |     ❌     |    ❌    |   ❌   |

<sub>Checked against each package's source, October 2026. ¹ conc re-raises a task's panic from `Wait` by design. ² errgroup returns the first error, no results. ³ errgroup has no queue; `TryGo` only refuses a task while every slot is busy. ⁴ pond subpools: each has its own queue and runs on the parent pool's workers.</sub>

### Performance

Every pool gets 50K workers and runs three kinds of task (sleep 10 ms, a short CPU loop, and a mix of both), 5M tasks of each, sent by anywhere from 1 to 1M producers. Charts for the sleep task:

<details>
<summary><b>24 CPUs</b></summary>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/bench/bench-24cpu-dark.png">
  <img alt="Execution time, throughput, memory and allocations of nine Go worker pools on 24 CPUs" src="./assets/bench/bench-24cpu-light.png">
</picture>

[Open live chart](https://goptics.github.io/varmq-benchmarks/?d=0&g=0)

</details>
<details>
<summary><b>8 CPUs</b></summary>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/bench/bench-8cpu-dark.png">
  <img alt="Execution time, throughput, memory and allocations of nine Go worker pools on 8 CPUs" src="./assets/bench/bench-8cpu-light.png">
</picture>

[Open live chart](https://goptics.github.io/varmq-benchmarks/?d=1&g=0)

</details>
<details>
<summary><b>4 CPUs</b></summary>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/bench/bench-4cpu-dark.png">
  <img alt="Execution time, throughput, memory and allocations of nine Go worker pools on 4 CPUs" src="./assets/bench/bench-4cpu-light.png">
</picture>

[Open live chart](https://goptics.github.io/varmq-benchmarks/?d=2&g=0)

</details>

The charts above show only the sleep task at 50K workers. For every task, workload, pool size and CPU count, see the [interactive charts](https://goptics.github.io/varmq-benchmarks/); the harness and raw results live in [varmq-benchmarks](https://github.com/goptics/varmq-benchmarks).

**Summary**: time to run all 15M tasks per CPU count and memory per 1M tasks, **lower is better**; throughput in million tasks per second, averaged over every run, **higher is better**.

| Pool             | 4 CPUs (s) | 8 CPUs (s) | 24 CPUs (s) | Throughput (M/s) | Memory (MB) |
| ---------------- | ---------: | ---------: | ----------: | ---------------: | ----------: |
| **VarMQ**        | **5.10** | **3.55** | **3.97** | **4.90**   | 115    |
| BufferedPool⁵    | 5.57     | 5.44     | 10.38    | 2.61       | 23     |
| ants             | 6.71     | 6.53     | 8.11     | 2.22       | 16     |
| GoroutinePool⁵   | 7.45     | 7.26     | 14.44    | 1.79       | **15** |
| conc             | 8.66     | 8.65     | 16.75    | 1.56       | **15** |
| workerpool       | 10.72    | 12.76    | 16.00    | 1.33       | 28     |
| pond v2          | 21.82    | 23.20    | 46.76    | 0.57       | 317    |

Why more memory: `Add` never blocks, so VarMQ queues every task as a job (ID, status, result handle) until a worker takes it. ants, conc and the unbuffered pool make the producer wait instead, so far fewer tasks are held at once.

<sub>⁵ Hand-rolled: fixed workers reading a channel, unbuffered (GoroutinePool) or with 1M slots (BufferedPool). Measured on an i7-13700.</sub>

### Micro-benchmarks

<details>
<summary>Cost of a single <code>Add</code>, <code>AddAll</code> and <code>Publish</code> call</summary>

```text
goos: linux
goarch: amd64
pkg: github.com/goptics/varmq
cpu: 13th Gen Intel(R) Core(TM) i7-13700
```

#### `Add`

Command: `go test -run=^$ -benchmem -bench '^(BenchmarkAdd)$' -cpu=1`

> Why use `-cpu=1`? Since the benchmark doesn’t test with more than 1 concurrent worker, a single CPU is ideal to accurately measure performance.

| Worker Type      | Queue Type     | Time (ns/op) | Memory (B/op) | Allocations (allocs/op) |
| ---------------- | -------------- | ------------ | ------------- | ----------------------- |
| **Worker**       | Queue          | 1,020        | 86            | 1                       |
|                  | Priority       | 1,057        | 64            | 1                       |
| **ErrWorker**    | ErrQueue       | 1,055        | 110           | 1                       |
|                  | ErrPriority    | 1,093        | 88            | 1                       |
| **ResultWorker** | ResultQueue    | 1,053        | 102           | 1                       |
|                  | ResultPriority | 1,093        | 80            | 1                       |

#### `AddAll`

Command: `go test -run=^$ -benchmem -bench '^(BenchmarkAddAll)$' -cpu=1`

| Worker Type      | Queue Type     | Time (ns/op) | Memory (B/op) | Allocations (allocs/op) |
| ---------------- | -------------- | ------------ | ------------- | ----------------------- |
| **Worker**       | Queue          | 144,307      | 86,163        | 1,004                   |
|                  | Priority       | 700,103      | 64,129        | 1,002                   |
| **ErrWorker**    | ErrQueue       | 171,741      | 126,701       | 1,507                   |
|                  | ErrPriority    | 745,010      | 104,669       | 1,505                   |
| **ResultWorker** | ResultQueue    | 181,409      | 143,299       | 1,007                   |
|                  | ResultPriority | 735,915      | 121,268       | 1,005                   |

> [!Note]
>
> `AddAll` benchmarks use a batch of **1000 items** per call. The reported numbers (`ns/op`, `B/op`, `allocs/op`) are totals for the whole batch. For per-item values, divide each by 1000.
> e.g. for default `Queue`, the average time per item is approximately **144ns**, against about **1,020ns** for a single `Add` followed by `Wait`.

Why is `AddAll` so much faster per item than individual `Add` calls?

1. **Batched dispatch**: with a thousand jobs queued at once, the event loop hands each worker its share of the backlog in one wake-up instead of one job per wake-up, so the hand-off cost is spread over the batch. In-memory FIFO queues and topics do this; priority queues deliberately take one job per hand-off (a higher-priority job must never wait behind a batch), which is why their `AddAll` gains much less.
2. **One wait, one group**: a single group job tracks the whole batch and the caller waits once, instead of a full enqueue, wake-up, run, wake-the-caller round trip per job.

#### `Publish`

Command: `go test -run=^$ -benchmem -bench '^(BenchmarkPublish)$' -cpu=1`

| Worker Type | Queue Type | Time (ns/op) | Memory (B/op) | Allocations (allocs/op) |
| ----------- | ---------- | ------------ | ------------- | ----------------------- |
| **Worker**  | Topic      | 126.8        | 76            | 1                       |

One worker subscribed to the topic. Unlike the `Add` benchmark, it does not wait for each job: it publishes `b.N` payloads and waits once at the end, so the number is per-payload throughput with batched dispatch, not a round trip.

</details>

## API Reference

For the complete package documentation, types, and method signatures, browse the **[GoDoc](https://pkg.go.dev/github.com/goptics/varmq)**.

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=goptics/varmq&type=Date)](https://www.star-history.com/#goptics/varmq&Date)

## Contributing

Issues and pull requests are welcome. To set up and run the checks locally:

```bash
make init  # install lefthook git hooks and tidy modules
make test  # go test -race ./...
make bench # micro-benchmarks
```

The Redis examples need a local Redis: `docker compose -f examples/compose.yml up -d`. Commit messages follow [Conventional Commits](https://www.conventionalcommits.org) (`feat:`, `fix:`, `docs:`, ...).

This project has a [Code of Conduct](CODE_OF_CONDUCT.md). By participating, you agree to its terms.

## License

[MIT](LICENSE)
