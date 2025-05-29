# VarMQ

[![Go Reference](https://img.shields.io/badge/go-pkg-00ADD8.svg?logo=go)](https://pkg.go.dev/github.com/goptics/varmq)
[![DeepWiki](https://img.shields.io/badge/DeepWiki-goptics%2Fvarmq-blue.svg?logo=data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAACwAAAAyCAYAAAAnWDnqAAAAAXNSR0IArs4c6QAAA05JREFUaEPtmUtyEzEQhtWTQyQLHNak2AB7ZnyXZMEjXMGeK/AIi+QuHrMnbChYY7MIh8g01fJoopFb0uhhEqqcbWTp06/uv1saEDv4O3n3dV60RfP947Mm9/SQc0ICFQgzfc4CYZoTPAswgSJCCUJUnAAoRHOAUOcATwbmVLWdGoH//PB8mnKqScAhsD0kYP3j/Yt5LPQe2KvcXmGvRHcDnpxfL2zOYJ1mFwrryWTz0advv1Ut4CJgf5uhDuDj5eUcAUoahrdY/56ebRWeraTjMt/00Sh3UDtjgHtQNHwcRGOC98BJEAEymycmYcWwOprTgcB6VZ5JK5TAJ+fXGLBm3FDAmn6oPPjR4rKCAoJCal2eAiQp2x0vxTPB3ALO2CRkwmDy5WohzBDwSEFKRwPbknEggCPB/imwrycgxX2NzoMCHhPkDwqYMr9tRcP5qNrMZHkVnOjRMWwLCcr8ohBVb1OMjxLwGCvjTikrsBOiA6fNyCrm8V1rP93iVPpwaE+gO0SsWmPiXB+jikdf6SizrT5qKasx5j8ABbHpFTx+vFXp9EnYQmLx02h1QTTrl6eDqxLnGjporxl3NL3agEvXdT0WmEost648sQOYAeJS9Q7bfUVoMGnjo4AZdUMQku50McDcMWcBPvr0SzbTAFDfvJqwLzgxwATnCgnp4wDl6Aa+Ax283gghmj+vj7feE2KBBRMW3FzOpLOADl0Isb5587h/U4gGvkt5v60Z1VLG8BhYjbzRwyQZemwAd6cCR5/XFWLYZRIMpX39AR0tjaGGiGzLVyhse5C9RKC6ai42ppWPKiBagOvaYk8lO7DajerabOZP46Lby5wKjw1HCRx7p9sVMOWGzb/vA1hwiWc6jm3MvQDTogQkiqIhJV0nBQBTU+3okKCFDy9WwferkHjtxib7t3xIUQtHxnIwtx4mpg26/HfwVNVDb4oI9RHmx5WGelRVlrtiw43zboCLaxv46AZeB3IlTkwouebTr1y2NjSpHz68WNFjHvupy3q8TFn3Hos2IAk4Ju5dCo8B3wP7VPr/FGaKiG+T+v+TQqIrOqMTL1VdWV1DdmcbO8KXBz6esmYWYKPwDL5b5FA1a0hwapHiom0r/cKaoqr+27/XcrS5UwSMbQAAAABJRU5ErkJggg==)](https://deepwiki.com/goptics/varmq)
[![Playground](https://img.shields.io/badge/Go_Playground-blue.svg)](https://go.dev/play/p/SxjdmUynIZB)
[![Go Report Card](https://goreportcard.com/badge/github.com/goptics/varmq)](https://goreportcard.com/report/github.com/goptics/varmq)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://golang.org/doc/devel/release.html)
[![CI](https://github.com/goptics/varmq/actions/workflows/go.yml/badge.svg)](https://github.com/goptics/varmq/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/goptics/varmq/branch/main/graph/badge.svg)](https://codecov.io/gh/goptics/varmq/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)

**VarMQ** is a high-performance message queue for Go that simplifies concurrent task processing using [worker pool](#the-concurrency-architecture). Using Go generics, it provides type safety without sacrificing performance.

With VarMQ, you can process messages asynchronously, handle errors properly, store data persistently, and scale across systems when needed. All through a clean, intuitive API that feels natural to Go developers.

This isn't meant to replace RabbitMQ or Kafka - VarMQ serves a different purpose as a lightweight, in-process message queue with strong worker management. For persistence and distribution, it offers a flexible [adapter system](#built-in-adapters) that extends its capabilities beyond simple in-memory queues.

## ✨ Features

- **⚡ High performance**: Optimized for throughput control with minimal overhead
- **🛠️ Variants of queue types**:
  - Standard queues for in-memory processing
  - Priority queues for importance-based ordering
  - Persistent queues for durability across restarts
  - Distributed queues for processing across multiple systems
- **🧩 Worker abstractions**:
  - `NewWorker` - Fire-and-forget operations (most performant)
  - `NewErrWorker` - Returns only error (when result isn't needed)
  - `NewResultWorker` - Returns result and error
- **🚦 Concurrency control**: Fine-grained control over worker pool size, dynamic tuning and idle workers management
- **💾 Persistence**: Support for durable storage through adapter interfaces
- **🌐 Distribution**: Scale processing across multiple instances via adapter interfaces
- **🧩 Extensible**: Build your own storage adapters by implementing simple interfaces

## Quick Start

### Installation

```bash
go get github.com/goptics/varmq
```

### Basic Usage

```go
package main

import (
    "fmt"
    "time"

    "github.com/goptics/varmq"
)

func main() {
  w := varmq.NewWorker(func(j varmq.Job[int]) {
    fmt.Printf("Processing %d\n", j.Data())
    time.Sleep(1 * time.Second)
  }, 10)
  defer w.WaitUntilFinished()
  q := w.BindQueue()

  for i := range 100 {
    q.Add(i)
  }
}
```

## Persistent and Distributed Queues

VarMQ supports both persistent and distributed queue processing through adapter interfaces:

- **Persistent Queues**: Store jobs durably so they survive program restarts
- **Distributed Queues**: Process jobs across multiple systems

Usage is simple:

```go
// For persistent queues (with any IPersistentQueue adapter)
queue := worker.WithPersistentQueue(persistentQueueAdapter)

// For distributed queues (with any IDistributedQueue adapter)
queue := worker.WithDistributedQueue(distributedQueueAdapter)
```

See complete working examples in the [examples directory](./examples):

- [Persistent Queue Example (SQLite)](./examples/sqlite-persistent)
- [Persistent Queue Example (Redis)](./examples/redis-persistent)
- [Distributed Queue Example (Redis)](./examples/redis-distributed)

Create your own adapters by implementing the `IPersistentQueue` or `IDistributedQueue` interfaces.

> Note: Before testing examples, make sure to start the Redis server using `docker compose up -d`.

## Advanced Features

### Priority Queues

Process important jobs first:

```go
// Create a standard priority queue
queue := worker.BindPriorityQueue()

// Add jobs with priorities (lower number = higher priority)
queue.Add("High priority", 1)
queue.Add("Low priority", 10)
```

## Benchmarks

```text
goos: linux
goarch: amd64
pkg: github.com/goptics/varmq
cpu: AMD EPYC 7763 64-Core Processor
```

| Benchmark Operation                         | Time (ns/op) | Memory (B/op) | Allocations (allocs/op) |
| :------------------------------------------ | :----------- | :------------ | :---------------------- |
| Queue Add                                   | 1217         | 112           | 3                       |
| **Queue AddAll (1000 items)**               | 810354       | 130185        | 4002                    |
| PriorityQueue Add                           | 1296         | 144           | 4                       |
| **PriorityQueue AddAll (1000 items)**       | 1078373      | 162177        | 5002                    |
| ErrWorker Add                               | 1391         | 288           | 6                       |
| **ErrWorker AddAll (1000 items)**           | 881515       | 154713        | 4505                    |
| ErrPriorityQueue Add                        | 1452         | 320           | 7                       |
| **ErrPriorityQueue AddAll (1000 items)**    | 1182968      | 186706        | 5505                    |
| ResultWorker Add                            | 1354         | 336           | 6                       |
| **ResultWorker AddAll (1000 items)**        | 864143       | 171320        | 4005                    |
| ResultPriorityQueue Add                     | 1450         | 368           | 7                       |
| **ResultPriorityQueue AddAll (1000 items)** | 1151502      | 203314        | 5005                    |

**Note:** `AddAll` benchmarks were performed by adding 1000 items in a single call. The reported `ns/op`, `B/op`, and `allocs/op` for `AddAll` are for the entire batch operation. To estimate per-item metrics for an `AddAll` operation, divide the table values by 1000 (e.g., for Queue AddAll, the average time per item is approximately 810 ns).

## WhyVarMQ?

- **Simple API**: Clean, intuitive interface that doesn't get in your way
- **Minimal Dependencies**: Core library has no external dependencies
- **Production Ready**: Built for real-world scenarios and high-load applications
- **Highly Extensible**: Create your own storage adapters by implementing VarMQ's [internal queue interfaces](./diagrams/interface.drawio.png)

### Built-in adapters

- ⚡ Redis: [redisq](https://github.com/goptics/redisq)
- 🗃️ SQLite: [sqliteq](https://github.com/goptics/sqliteq)
- 🦆 DuckDB: [duckq](https://github.com/goptics/duckq)
- 🐘 PostgreSQL: 🔄 Upcoming

## API Reference

For detailed API documentation, see the **[API Reference](./docs/API_REFERENCE.md.md)**.

## The Concurrency Architecture

VarMQ primarily uses its own Event-Loop internally to handle concurrency.

This event loop checks if there are any pending jobs in the queue and if any workers are available in the worker pool. If there are, it distributes jobs to all available workers and then goes back into sleep mode.

When a worker becomes free, it then send pull job request to the event loop.

The event loop then checks again if there are any pending jobs in the queue. If there are, it continues to distribute them to the workers. otherwise, the idle workers been removed from the pool automatically or stay based on `WorkerConfig`.

![varmq architecture](./diagrams/varmq.excalidraw.png)

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 👤 Author

- GitHub: [@fahimfaisaal](https://github.com/fahimfaisaal)
- LinkedIn: [in/fahimfaisaal](https://www.linkedin.com/in/fahimfaisaal/)
- Twitter: [@FahimFaisaal](https://twitter.com/FahimFaisaal)
