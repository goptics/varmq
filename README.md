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
    <a href="#delivery-guarantees">Guarantees</a> ·
    <a href="#comparison-and-benchmarks">Comparison</a> ·
    <a href="#contributing">Contributing</a>
  </p>

  <p>
    A typed worker pool and job queue for Go. Each worker is a concurrent pool that pulls jobs from any mix of in-memory queues, priority queues, <a href="#shared-storage-redis-sqlite">shared storage</a> (Redis, SQLite) and <a href="#topic-fan-out">topics</a>.
  </p>
</div>

## Features

- **High performance**: lock-free appends and [batched dispatch](#how-it-works). [See benchmarks](#performance).
- **Queue types**: standard and priority, in memory or in shared storage (Redis, SQLite) that survives restarts and serves many processes.
- **Worker types**: `NewWorker` (no return value), `NewErrWorker` (error only), `NewResultWorker` (result and error).
- **Concurrency control**: resize the pool at runtime, pause, resume, restart.
- **Multi queue binding**: one worker, many queues, a pluggable selection strategy.
- **Topic fan-out**: publish once, every subscribed worker gets its own job.
- **Workflows**: a DAG of jobs over queues with typed results, `Always` and saga `Undo`.
- **Jobs**: deduplicate by ID, cancel, or wait for a result, from any process on shared storage. [Jobs](https://varmq.goptics.org/concepts/jobs/).
- **Queue pause**: pause a queue for every worker in every process. [Queues](https://varmq.goptics.org/guides/queues/).
- **Function helpers**: submit plain `func()` values, like pond or ants. [Function helpers](https://varmq.goptics.org/guides/function-helpers/).
- **REST API and Prometheus metrics**: inspect and control workers, queues, jobs and workflows over HTTP. [REST API](https://varmq.goptics.org/operations/rest-api/).
- **Dashboard**: [vartop](https://github.com/goptics/vartop) shows every process of a service in one place, with history, and acts on workers, queues, jobs and workflows.
- **Extensible**: plug in your own storage with [`IQueue`](https://pkg.go.dev/github.com/goptics/varmq#IQueue) or [`IPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPriorityQueue). [Adapters](https://varmq.goptics.org/persistence/adapters/).

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

Runnable programs live in [examples](./examples). Every topic below has a full page in the **[docs](https://varmq.goptics.org/)**.

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

↗️ **[Run it on Playground](https://go.dev/play/p/uP0rA-NrzZB)** · [Concurrency](https://varmq.goptics.org/guides/concurrency/) · [Lifecycle](https://varmq.goptics.org/concepts/lifecycle/)

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
```

↗️ **[Run it on Playground](https://go.dev/play/p/nKnAyQfkxou)** · [Worker types](https://varmq.goptics.org/guides/worker-types/)

### Multi Queue Binds

Bind several queues to one worker. `varmq.WithStrategy` picks the next queue: `Priority` (default, bind order), `RoundRobin`, `MaxLen` or `MinLen`.

```go
worker := varmq.NewWorker(func(j varmq.Job[string]) {
	fmt.Println("Processing:", j.Data())
}, varmq.WithStrategy(varmq.RoundRobin))
defer worker.WaitUntilIdle()

q1 := worker.BindQueue()
q2 := worker.BindQueue()
pq := worker.BindPriorityQueue()
```

↗️ **[Run it on Playground](https://go.dev/play/p/0eL_0WNRVIh)** · [Strategies](https://varmq.goptics.org/guides/strategies/)

With `varmq.RoundRobin`, empty queues are skipped:

![VarMQ multiple queues with round-robin selection](./assets/diagrams/varmq-multi-queue.svg)

### Topic (Fan-out)

Publish once and every subscribed worker gets its own job. A late subscriber sees only what is published after it subscribed.

```go
topic := varmq.NewTopic[int]()

email.WithTopic(topic)
stock.WithTopic(topic)

for order := range 4 {
	topic.Publish(order) // both workers get all 4 orders
}
```

↗️ **[Run it on Playground](https://go.dev/play/p/NeIa69kRi5s)** · [Topics](https://varmq.goptics.org/guides/topics/)

![VarMQ topic fan-out with two subscribed workers](./assets/diagrams/varmq-topic.svg)

### Shared Storage (Redis, SQLite)

Same `WithQueue` / `WithPriorityQueue`. Jobs go out as JSON, survive restarts, and any process bound to the same queue runs them. Results come back to whichever process waits.

```go
rq := redisq.New(url)

// consumer process
w := varmq.NewResultWorker(func(j varmq.Job[int]) (string, error) { ... })
w.WithQueue(rq.NewQueue("thumbs"))

// producer process: no worker
q := varmq.NewResultQueue[int, string](rq.NewQueue("thumbs"))
job, _ := q.Add(64)
s, err := job.Result() // waits, whichever process ran it
```

Adapters: ⚡ [redisq](https://github.com/goptics/redisq) · 🗃️ [sqliteq](https://github.com/goptics/sqliteq) · 🐘 PostgreSQL (upcoming). Check the [versions table](https://varmq.goptics.org/start/installation/#versions) first.

Examples: [SQLite](./examples/sqlite-persistent), [Redis, one process](./examples/redis-persistent), [Redis, producer and consumer](./examples/redis-distributed). Docs: [Persistence](https://varmq.goptics.org/persistence/overview/).

### Workflows

A DAG of jobs over queues, with typed results, `Always()` and saga `Undo`. Needs Go 1.27.

```go
wf := varmq.NewWorkflow(varmq.NewWorkflowStore(), varmq.WithWorkflowID("upload-1"))
up := wf.Add("upload", uploadQ, "photo.png")
s := wf.Add("thumb-s", thumbQ, 64).After(up)
l := wf.Add("thumb-l", thumbQ, 512).After(up)
wf.Add("email", mailQ, "ann@example.com").
	After(s, l).
	Always().
	Undo(unsendQ, "ann@example.com")

wf.Submit(ctx)                // idempotent per ID
rec, _ := wf.WaitContext(ctx) // rec.State: completed, failed, compensated, ...
```

Example: [`examples/workflow`](./examples/workflow). Docs: [Workflows](https://varmq.goptics.org/guides/workflows/).

### REST API

```go
mux := http.NewServeMux()
mux.Handle("/varmq/", varmq.Handler("/varmq"))
```

> [!Warning]
> The REST API has no authentication. It can read job payloads, stop workers and cancel jobs. Protect it before you expose it.

Routes and responses: [REST API](https://varmq.goptics.org/operations/rest-api/), [`spec/openapi.yaml`](spec/openapi.yaml).

### Connect to vartop (hub)

A process can dial out to a [vartop](https://github.com/goptics/vartop) hub instead of serving the REST API on a port. It redials with backoff and returns when `ctx` ends.

```go
go varmq.ConnectHub(ctx, "https://vartop.internal", token,
	varmq.WithHubErrHandler(func(err error) { log.Printf("hub: %v", err) }),
)
```

> [!Warning]
> Whoever answers at the hub URL gets everything that `Handler` allows. Use TLS. `WithHubReadOnly()` and `WithHubNoPayloads()` limit what the hub can do and see.

Options, trust and sessions: [Dashboard (vartop)](https://varmq.goptics.org/operations/vartop/). Protocol: [`spec/hub.md`](spec/hub.md).

## How It Works

A single event loop sleeps until something signals it: `Add()` putting a job in the queue, or a worker finishing one. On each signal it hands jobs to workers while the queue has work and a worker slot is free, then goes back to waiting.

![VarMQ event loop with one worker](./assets/diagrams/varmq-single-worker.svg)

The signal channel holds one pending signal, so extra signals from many `Add` calls are dropped. One wake-up is enough. Concurrency is a limit, not a fixed pool size: goroutines start while jobs wait and exit as the queue drains.

![VarMQ worker pool with concurrency 4](./assets/diagrams/varmq-worker-pool.svg)

With many short jobs, the event loop becomes the bottleneck. So a worker can take a batch of jobs from an in-memory queue or topic and run them in order. Below, 8 cheap jobs with concurrency 2: the first worker takes 4, a second worker is started for 2, and the last 2 go out one at a time. Priority queues, shared storage and multi-queue workers do not batch.

![VarMQ batched dispatch](./assets/diagrams/varmq-batch.svg)

Batch size rules and shared-storage dispatch: [How it works](https://varmq.goptics.org/concepts/how-it-works/).

## Delivery Guarantees

| Where | Guarantee | Notes |
| --- | --- | --- |
| In-memory queue | At most once | Jobs live in the process and are lost if it exits. |
| Shared storage | At least once | A job comes back if its consumer dies or its acknowledgment is lost. Make handlers safe to run twice. |
| `Add` with `WithJobId` on redisq or sqliteq | Stored once per ID | A retried `Add` with the same ID is stored once. The job can still run twice. |
| Topic | At most once, per subscription | Each subscribed worker gets every payload published after it subscribed. |

Cancel is cooperative: a running job stops only if its handler watches `j.Context()`. VarMQ does not retry a failed job. Details: [Persistence](https://varmq.goptics.org/persistence/overview/#guarantees-at-a-glance).

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

Run of 2026-10-25 on an i7-13700 (24 threads), Go 1.26, varmq v1.10.0. Every pool gets 50K workers and runs 15M tasks (sleep 10 ms, a short CPU loop, and a mix), sent by 1 to 1M producers. Time to run all tasks and memory per 1M tasks, **lower is better**; throughput, **higher is better**.

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

<sub>⁵ Hand-rolled: fixed workers reading a channel, unbuffered (GoroutinePool) or with 1M slots (BufferedPool). VarMQ uses more memory because `Add` never blocks, so it holds every waiting task as a job.</sub>

- **Redis**: VarMQ with redisq against Asynq, rmq, taskq and gocraft/work, 100 producers: up to 64K jobs/s and the lowest p50 latency (0.058 ms). gocraft/work leads with one instance and 10 workers, rmq with one producer. ↗️ [Live chart](https://benchmarks.goptics.org/?id=redis)
- **SQLite**: VarMQ with sqliteq against River, goqite and backlite: 8.6K jobs/s mixed and 14.7K drain, against goqite's 3K. ↗️ [Live chart](https://benchmarks.goptics.org/?id=sqlite)

Every row, the groups VarMQ does not win, and micro-benchmarks: [Benchmarks](https://varmq.goptics.org/reference/benchmarks/).

## API Reference

For the complete package documentation, types, and method signatures, browse the **[GoDoc](https://pkg.go.dev/github.com/goptics/varmq)**.

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
