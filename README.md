<div align="center">
<p>
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/text-logo-dark.svg">
  
  <source media="(prefers-color-scheme: light)" srcset="./assets/text-logo.svg">
  
  <img alt="VarMQ" width="400px" src="./assets/text-logo.svg">
</picture>
</p>


  <p>
    <a href="https://varmq.goptics.org/"><img src="https://img.shields.io/badge/Docs-blue?logo=readthedocs&logoColor=white" alt="Docs" /></a>
    <a href="https://github.com/avelino/awesome-go?tab=readme-ov-file#messaging"><img src="https://awesome.re/mentioned-badge-flat.svg" alt="Mentioned in Awesome Go" /></a>
    <a href="https://pkg.go.dev/github.com/goptics/varmq"><img src="https://img.shields.io/badge/go-pkg-00ADD8.svg?logo=go" alt="Go Reference" /></a>
    <a href="https://github.com/goptics/varmq/actions/workflows/ci.yml"><img src="https://github.com/goptics/varmq/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
    <a href="https://codecov.io/gh/goptics/varmq"><img src="https://codecov.io/gh/goptics/varmq/branch/main/graph/badge.svg" alt="Codecov" /></a>
    <a href="https://varmq.goptics.org/bench-log"><img src="https://img.shields.io/badge/Bench_Logs-orange?logo=googledocs&logoColor=white" alt="Bench Logs" /></a>
    <a href="https://golang.org/doc/devel/release.html"><img src="https://img.shields.io/badge/Go-1.27+-00ADD8?style=flat-square&logo=go" alt="Go Version" /></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square" alt="License" /></a>
  </p>

  <p align="center">
    <a href="https://varmq.goptics.org/">Docs</a> ·
    <a href="#features">Features</a> ·
    <a href="#quick-start">Quick Start</a> ·
    <a href="#usage">Usage</a> ·
    <a href="#how-it-works">How It Works</a> ·
    <a href="#trade-offs">Trade-offs</a> ·
    <a href="#delivery-guarantees">Guarantees</a> ·
    <a href="#comparison-and-benchmarks">Comparison</a> ·
    <a href="#api-reference">API Reference</a> ·
    <a href="#contributing">Contributing</a>
  </p>

  <p>
    A typed worker pool and job queue for Go. Each worker is a concurrent pool that pulls jobs from any mix of in-memory queues, priority queues, <a href="#shared-storage-redis-sqlite">shared storage</a> (Redis, SQLite) and <a href="#topic-fan-out">topics</a>.
  </p>
</div>

## Features

- **High performance**: lock-free appends and [batched dispatch](#batched-dispatch). [See benchmarks](#performance).
- **Queue types**: standard and priority, in memory or in shared storage (Redis, SQLite) that survives restarts and serves many processes.
- **Worker types**: `NewWorker` (no return value), `NewErrWorker` (error only), `NewResultWorker` (result and error).
- **Concurrency control**: resize the pool at runtime, pause, resume, restart.
- **Multi queue binding**: one worker, many queues, a pluggable selection strategy.
- **Topic fan-out**: publish once, every subscribed worker gets its own job.
- **Workflows**: a DAG of jobs over queues with typed results, `Always` and saga `Undo`.
- **Job cancel and outcome**: cancel a job, or wait for its result, from any process on shared storage.
- **Queue pause**: pause a queue for every worker in every process; the pause survives a restart.
- **REST API**: inspect and control workers, queues, jobs and workflows over HTTP.
- **Extensible**: plug in your own storage by implementing [`IQueue`](https://pkg.go.dev/github.com/goptics/varmq#IQueue) or [`IPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPriorityQueue). Optional interfaces add acknowledgments, wake-ups and pause (`varmq`), and outcomes, cancel and inspection ([`contract`](https://pkg.go.dev/github.com/goptics/varmq/contract)).

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

### Shared Storage (Redis, SQLite)

Same `WithQueue` / `WithPriorityQueue`. Storage that acknowledges (`IAcknowledgeable`) is detected: jobs go out as JSON, survive restarts when the storage persists them, and any process bound to the same queue runs them. Storage that notifies (`ISubscribable`, e.g. redisq) wakes idle workers at once; other storage is polled.

```go
rq := redisq.New(url)

// consumer process
w := varmq.NewResultWorker(func(j varmq.Job[int]) (string, error) { ... })
w.WithQueue(rq.NewQueue("thumbs"))

// producer process: no worker
q := varmq.NewResultQueue[int, string](rq.NewQueue("thumbs")) // NewQueue / NewErrQueue for Worker / ErrWorker jobs
job, _ := q.Add(64)
s, err := job.Result() // waits, whichever process ran it

// any process, by ID
j := varmq.OpenResultJob[string](rq.NewQueue("thumbs"), job.ID()) // OpenJob / OpenErrJob for the other workers
s, err = j.Result()
```

Needs redisq v1.6.0+ or sqliteq v0.4.0+. Older releases (redisq v1.5.1, sqliteq v0.3.0) lack `Reject` and do not compile with this VarMQ. See the [versions table](https://varmq.goptics.org/start/installation/#versions).

On a bound handle, `Wait`, `Err` and `Result` work as with in-memory queues when the storage reports outcomes (`OutcomeWatcher`: redisq and sqliteq). A failed job's `Err` is a `*varmq.RemoteError`, with the error text only. A result over 64 KiB is dropped (`ErrNoResult`). Custom storage without `OutcomeWatcher` gives `ErrDetached`. A job added without an ID gets a random one, so its outcome can be matched.

Examples: [SQLite](./examples/sqlite-persistent), [Redis, one process](./examples/redis-persistent), [Redis, producer and consumer](./examples/redis-distributed).

When the adapter deduplicates pushes (`EnqueueOnce`: redisq and sqliteq), give each job a unique ID such as a UUID. A retried `Add` with the same ID is then stored once:

```go
id := uuid.NewString() // once, so a retry carries the same ID
for {
	if _, ok := queue.Add(data, varmq.WithJobId(id)); ok {
		break
	}
	// back off and retry
}
```

#### Built-in adapters

- ⚡ Redis: [redisq](https://github.com/goptics/redisq)
- 🗃️ SQLite: [sqliteq](https://github.com/goptics/sqliteq)
- 🐘 PostgreSQL: 🔄 Upcoming

### Cancel a Job

`job.Cancel()` works on every in-memory handle. On shared storage it works when the storage can cancel: redisq and sqliteq can. Custom storage implements `JobCanceller`, plus `CancelNotifier` to reach running jobs and `CancelAcknowledgeable` to end them cancelled. Other storage gives `ErrCancelUnsupported`. A job that has not started never runs. A running job has its context cancelled, so it stops if the worker watches `j.Context()`:

```go
w := varmq.NewErrWorker(func(j varmq.Job[Img]) error {
	return resize(j.Context(), j.Data()) // returns once cancelled
})
q := w.WithQueue(rq.NewQueue("imgs"))

job, _ := q.Add(img)
job.Cancel()                                             // or, from any process:
varmq.OpenErrJob(rq.NewQueue("imgs"), job.ID()).Cancel() // by the job's ID
errors.Is(job.Err(), varmq.ErrJobCancelled)              // true, unless it finished first
```

A workflow task waiting in its queue is stopped with `Workflow.Cancel`. `Cancel` by ID reaches a running one.

### Pause a Queue

`worker.Pause()` stops one worker. `q.Pause()` stops every worker in every process from taking jobs from the queue, until `q.Resume()`. Adds still land and running jobs finish. On shared storage the pause is kept in storage, so it survives a restart.

```go
q := w.WithQueue(rq.NewQueue("emails"))

if err := q.Pause(); err != nil { // no worker takes jobs from "emails"
	return err // ErrPauseUnsupported when the storage can't pause
}
q.IsPaused() // true, in every process bound to "emails"
q.Resume()   // workers take jobs again
```

In-memory queues, redisq and sqliteq can pause. Other storage returns `ErrPauseUnsupported`. A paused Redis queue stops only consumers that run this VarMQ, so upgrade every consumer first.

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

### Workflows

A workflow is a DAG of jobs over queues. A task is queued the moment its last dependency ends, in the same atomic storage step. `Always()` runs a task even if a dependency failed. `Undo` registers a compensation, run in reverse order if the workflow fails or is cancelled (`WithUndoOnCancel(false)` opts out). Needs Go 1.27.

```go
wf := varmq.NewWorkflow(varmq.NewWorkflowStore(), varmq.WithWorkflowID("upload-1"))
up := wf.Add("upload", uploadQ, "photo.png")
s := wf.Add("thumb-s", thumbQ, 64).After(up)
l := wf.Add("thumb-l", thumbQ, 512).After(up)
wf.Add("email", mailQ, "ann@example.com").
	After(s, l).
	Always().
	Undo(unsendQ, "ann@example.com")

wf.Submit(ctx)            // idempotent per ID
rec, _ := wf.WaitContext(ctx) // or wf.Wait(); rec.State: completed, failed, compensated, ...

// in a worker: read a dependency's result
var src string
varmq.TaskOf(j).Result("upload", &src)
```

Cancel with `wf.Cancel(ctx)`; `varmq.OpenWorkflow(store, id)` reaches a workflow from any process on the same store, and its `Cancel` follows the workflow's `WithUndoOnCancel`. Runnable example: [`examples/workflow`](./examples/workflow). Storage rules: [spec §6.9](./spec/redis-storage.md#69-submit-a-workflow-since-150).

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
> Functions can't be serialized, so function helpers don't work with shared storage.

### Lifecycle and Errors

`Pause`, `Resume`, `TunePool` and `Stop` return at once. `Restart` waits for the worker to stop before starting it again, and the `Wait*` methods block until their condition is met. Running jobs always finish.

```go
worker := varmq.NewWorker(process, 4,
	varmq.WithContext(ctx), // cancelling ctx stops the worker
	varmq.WithErrHandler(func(w varmq.Worker, err error) {
		log.Println("varmq:", err) // dequeue, parse and other internal errors
	}),
)

worker.Pause()     // stop taking new jobs; unstarted jobs stay queued
worker.Resume()    // take new jobs again
worker.TunePool(8) // change concurrency while it runs
worker.Restart()   // stop and start again, keeping pending jobs
worker.Stop()      // graceful stop

worker.Wait()           // block until no job is running or pending
worker.WaitContext(ctx) // the same, until ctx ends (ctx.Err() then); jobs and groups have it too
worker.WaitUntilIdle()  // block until the worker is Idle (never returns while paused or stopped)
```

`Status()` reports the state (`Running`, `Idle`, `Paused`, `Stopped`, ...). `PauseAndWait`, `StopAndWait`, `WaitUntilPaused` and `WaitUntilStopped` block until a state is reached.

### REST API

Mount the handler on any `http.ServeMux`:

```go
mux := http.NewServeMux()
mux.Handle("/varmq/", varmq.Handler("/varmq"))
http.ListenAndServe(":8080", mux)
```

| Method  | Path                                       | Description                  |
|---------|--------------------------------------------|------------------------------|
| `GET`   | `/health`                                  | Health check                 |
| `GET`   | `/workers`                                 | List all workers (summary)   |
| `GET`   | `/workers/{name}`                          | Get worker details + metrics |
| `PATCH` | `/workers/{name}/actions/{action}`         | Pause, resume, stop, restart |
| `PATCH` | `/workers/{name}/config/concurrency/{n}`   | Adjust concurrency           |
| `GET`   | `/queues`, `/queues/{id}/jobs`             | List queues and their jobs   |
| `GET`   | `/queues/{id}/jobs/{jobId}`                | Read one job                 |
| `PATCH` | `/queues/{id}/actions/{action}`            | Pause, resume a queue        |
| `PATCH` | `/queues/{id}/jobs/{jobId}/actions/cancel` | Cancel a job                 |
| `GET`   | `/workflows`, `/workflows/{id}`            | List and read workflows      |
| `PATCH` | `/workflows/{id}/actions/cancel`           | Cancel a workflow            |

Job routes show one job (state, payload, attempts, times, errors, result) and cancel it. Cancel is cooperative: a waiting job never runs, and a running one stops if its worker watches `j.Context()`.

- In-memory queues of named workers serve both routes for jobs with an ID, waiting or running. A running job's payload is `null`, because its worker may change it.
- In-memory queues keep no ended jobs unless bound with `varmq.WithEndedJobs(n)` (n up to 200, default 0).
- A job can start while the route reads it. If you use the job routes, don't change `Data()` in place.
- Shared storage serves the read route when it implements `JobReader`, and cancel when it implements `JobCanceller`. redisq and sqliteq cancel jobs but answer 501 to a read.

Full contract: [`spec/openapi.yaml`](spec/openapi.yaml).

> [!Warning]
> The REST API is unauthenticated by design. Protect it with middleware, network isolation, or a reverse proxy before you expose it to untrusted networks. It serves job payloads and results. It can stop and resize workers, pause queues in every process, and cancel jobs and workflows.
>
> On shared storage, a job cancel reaches every consumer process. It marks the ID for 24 h, and a re-add of the ID clears the mark. It answers 202 for any ID with no ended record, and 409 for a retained ended job. A waiting workflow task answers 202 but still runs: cancel its workflow instead.

## How It Works

### Event loop

A single event loop sleeps until something signals it: `Add()` putting a job in the queue, or a worker finishing one. On each signal it hands jobs to workers while the queue has work and a worker slot is free, then goes back to waiting.

![VarMQ event loop with one worker](./assets/diagrams/varmq-single-worker.svg)

### Worker pool

Every `Add` (and every job in `AddAll`) tries to wake the event loop. The signal channel holds only one pending signal, so the extra signals are dropped. One wake-up is enough. The loop keeps giving out jobs while the queue has work and a worker slot is free. It gives each worker its share of the backlog as one batch, not one job per wake-up.

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
| Shared storage that acknowledges jobs | No | A worker must not hold unacknowledged jobs that other consumers could take. See [Shared storage](#shared-storage). |
| Worker bound to several queues | No | The strategy picks a queue for every single job. |

### Shared storage

A worker bound to shared storage takes jobs as JSON. It ends each job in storage: `Acknowledge` on success, or `Reject` with the error text on an error or panic.

- **Wake-up.** Storage that notifies (`ISubscribable`, redisq) wakes an idle worker as soon as any process adds a job. Other storage is polled, and an add in the same process wakes the worker at once.
- **Credit dispatch.** When the storage pops in batches (`IBatchAcknowledgeable`: redisq, sqliteq) and is the only source, one pop fills every free slot. The limit is 64 jobs. Each job goes straight to a goroutine, so no worker holds jobs it has not started.
- **Pause.** A paused queue gives out nothing. A `Resume` from any process wakes the workers.
- **Cancel.** A cancel reaches the worker that runs the job (`CancelNotifier`) and cancels its `j.Context()`. A job that ends cancelled is never retried.

## Trade-offs

- **In-memory jobs are lost on a crash.** In-memory queues live in the process. Use shared storage for jobs you can't lose.
- **At least once on shared storage.** A job can run again when its process dies mid-job or its acknowledgment is lost. Make handlers safe to run twice.
- **Cancel is cooperative.** A running job stops only if its handler watches `j.Context()`.
- **Outcomes need storage support.** A remote result is capped at 64 KiB. An outcome can be lost when the storage drops its record before a watcher reads it, so wait with `WaitContext` and a timeout.

## Delivery Guarantees

| Where | Guarantee | Notes |
| --- | --- | --- |
| In-memory queue | At most once | Jobs live in the process and are lost if it exits. |
| Shared storage that acknowledges jobs | At least once | A job comes back if its consumer dies or its acknowledgment is lost. See the adapter's own guarantees. |
| `Add` with `WithJobId` on storage with `EnqueueOnce` | Once per ID, within the dedupe window | A retried `Add` with the same ID is stored once while the adapter keeps the ID. The job can still run twice (at least once). |
| Topic | At most once, per subscription | Each subscribed worker gets every payload published after it subscribed. |

<a id="benchmarks"></a>

## Comparison and Benchmarks

VarMQ is compared with [ants](https://github.com/panjf2000/ants), [pond v2](https://github.com/alitto/pond), [conc](https://github.com/sourcegraph/conc), [workerpool](https://github.com/gammazero/workerpool), [errgroup](https://pkg.go.dev/golang.org/x/sync/errgroup), [gopool](https://github.com/bytedance/gopkg/tree/main/util/gopool) and two hand-rolled channel pools as a worker pool. It is also compared with Redis and SQLite job queues as shared storage. The numbers come from the [benchmark repository](https://github.com/goptics/benchmarks).

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
| Task counters (completed, failed, ...)   |  ✅   |  ❌  |   ✅    |  ❌  |     ❌     |    ❌    |   ❌   |
| Built-in REST API                        |  ✅   |  ❌  |   ❌    |  ❌  |     ❌     |    ❌    |   ❌   |

<sub>Checked against each package's source, October 2026. ¹ conc re-raises a task's panic from `Wait` by design. ² errgroup returns the first error, no results. ³ errgroup has no queue; `TryGo` only refuses a task while every slot is busy. ⁴ pond subpools: each has its own queue and runs on the parent pool's workers.</sub>

### Performance

Run of 2026-10-25 on an i7-13700 (24 threads), Go 1.26, varmq v1.10.0. Raw results and harness in [goptics/benchmarks](https://github.com/goptics/benchmarks), charts at [benchmarks.goptics.org](https://benchmarks.goptics.org).

#### Worker pool

Every pool gets 50K workers and runs three kinds of task (sleep 10 ms, a short CPU loop, and a mix of both), 5M tasks of each, sent by anywhere from 1 to 1M producers. Live charts: ↗️ [24 CPUs](https://benchmarks.goptics.org/?id=pool-24cpu) · ↗️ [8 CPUs](https://benchmarks.goptics.org/?id=pool-8cpu) · ↗️ [4 CPUs](https://benchmarks.goptics.org/?id=pool-4cpu).

Time to run all 15M tasks per CPU count and memory per 1M tasks, **lower is better**; throughput in million tasks per second, averaged over every run, **higher is better**.

| Pool             | 4 CPUs (s) | 8 CPUs (s) | 24 CPUs (s) | Throughput (M/s) | Memory (MB) |
| ---------------- | ---------: | ---------: | ----------: | ---------------: | ----------: |
| **VarMQ**        | **5.07** | **3.54** | **3.85** | **4.89**   | 115    |
| BufferedPool⁵    | 5.43     | 5.41     | 8.49     | 2.73       | 23     |
| errgroup         | 6.38     | 5.69     | 9.23     | 2.29       | 111    |
| ants             | 6.74     | 6.51     | 8.00     | 2.22       | 15     |
| gopool           | 7.13     | 7.48     | 11.00    | 1.92       | 52     |
| GoroutinePool⁵   | 7.24     | 6.96     | 12.85    | 1.90       | **14** |
| conc             | 8.45     | 8.61     | 17.38    | 1.56       | 15     |
| workerpool       | 10.20    | 11.58    | 16.42    | 1.35       | 28     |
| pond v2          | 20.67    | 22.23    | 42.38    | 0.60       | 318    |

Why more memory: `Add` never blocks, so VarMQ queues every task as a job (ID, status, result handle) until a worker takes it; errgroup starts a goroutine per task. ants, conc and the unbuffered pool make the producer wait instead, so far fewer tasks are held at once.

The full matrix (100K, 300K and single-worker pools) is on the live charts; the harness is in [pool](https://github.com/goptics/benchmarks/tree/main/pool).

<sub>⁵ Hand-rolled: fixed workers reading a channel, unbuffered (GoroutinePool) or with 1M slots (BufferedPool).</sub>

#### Redis-backed queues

<details>
<summary>Throughput and latency against Asynq, rmq, taskq and gocraft/work</summary>

VarMQ with [redisq](https://github.com/goptics/redisq) against [Asynq](https://github.com/hibiken/asynq), [rmq](https://github.com/wellle/rmq), [taskq](https://github.com/vmihailenco/taskq) and [gocraft/work](https://github.com/gocraft/work). Each run moves 100K no-op jobs from 100 producers, with one library instance (C1) or four in one process (C4); Redis 7.2 is pinned to its own core. ↗️ [Live chart](https://benchmarks.goptics.org/?id=redis).

Jobs per second, **higher is better**; enqueue-to-handler latency, **lower is better**.

| Library      | C1, 10 workers | C1, 100 workers | C4, 10 workers | C4, 100 workers | Latency p50 (ms) |
| ------------ | -------------: | --------------: | -------------: | --------------: | ---------------: |
| **VarMQ**⁶   | 41,816         | **59,898**      | 54,371         | **64,352**      | **0.058**        |
| gocraft/work | **48,917**     | 44,775          | 47,484         | 42,707          | 0.082            |
| taskq        | 34,434         | 34,499          | 18,989         | 15,898          | 0.062            |
| rmq          | 29,616         | 28,521          | **60,003**     | 59,094          | 1.25             |
| Asynq        | 17,823         | 17,744          | 17,397         | 17,670          | 998⁷             |

With a single producer, rmq is fastest in every row (about 35K jobs/s against VarMQ's 21K). Every row, including the single-producer ones, is in [redis](https://github.com/goptics/benchmarks/tree/main/redis).

<sub>⁶ C1 uses a persistent queue, C4 a distributed queue (a persistent queue serves one instance); latency is the persistent queue, 0.068 ms for the distributed one. ⁷ Asynq sleeps 0.5 to 1.5 s after an empty poll.</sub>

</details>

#### SQLite-backed queues

<details>
<summary>Throughput against River, goqite and backlite</summary>

VarMQ with [sqliteq](https://github.com/goptics/sqliteq) against [River](https://github.com/riverqueue/river) (SQLite driver), [goqite](https://github.com/maragudk/goqite) and [backlite](https://github.com/mikestefanello/backlite), all in one process on WAL. Mixed produces and consumes at once; Drain adds every job first, then times the consumers; Prio adds priorities; 10Q spreads the jobs over 10 queues. 20K jobs per run (10K for Prio), SQLite's default checkpoint of 1000 pages. ↗️ [Live chart](https://benchmarks.goptics.org/?id=sqlite).

Jobs per second, **higher is better**.

| Library             | Mixed     | Drain      | Drain, Prio | Mixed, 10Q |
| ------------------- | --------: | ---------: | ----------: | ---------: |
| **VarMQ**           | **8,575** | **14,725** | **12,937**  | **29,624**⁸ |
| VarMQ, one file     |           |            |             | 4,553      |
| goqite              | 3,032     | 2,933      | 2,908       | 2,131      |
| River               | 1,375     | 3,230      | 3,240       | 1,431      |
| backlite            | 924       | 1,119      |             | 5,326      |

VarMQ is also fastest at a 10000-page checkpoint. Both checkpoint sizes and WAL sizes are in [sqlite](https://github.com/goptics/benchmarks/tree/main/sqlite).

<sub>⁸ One database file per queue, sqliteq's default. The other libraries keep every queue in one file; so does "VarMQ, one file" (`WithSingleFile`). backlite does not run Prio.</sub>

</details>

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
