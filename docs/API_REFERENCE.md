# VarMQ API Reference

Welcome to the VarMQ API reference. VarMQ is a powerful Go library for creating concurrent job queues, enabling robust and scalable background task processing.

## Core Concepts

- **Workers**: Entities that execute tasks. You define the task logic as a function.
- **Jobs**: Represent individual tasks to be processed. Each job carries data and progresses through a lifecycle (e.g., Created, Queued, Processing, Finished).
- **Queues**: Structures that hold jobs before they are picked up by workers. VarMQ supports various queue types (standard, priority, persistent, distributed).
- **Concurrency**: VarMQ manages a pool of worker goroutines to process jobs concurrently. The level of concurrency is configurable.

## Worker Creation

VarMQ provides three primary functions to create workers, tailored for different job outcomes:

### 1. `NewWorker`

Creates a general-purpose worker. The worker function provided to `NewWorker` does not explicitly return a value or error; its interaction is primarily through the `Job[T]` argument it receives. This worker type is versatile and is the **only type that can be bound to persistent and distributed queues**.

```go
func NewWorker[T any](wf func(j Job[T]), config ...any) IWorkerBinder[T]
```

- **`wf func(j Job[T])`**: Your function that defines the work. `j Job[T]` provides access to job data and ID.
- **`config ...any`**: Optional configurations (e.g., `varmq.WithConcurrency(4)` or simply `4`).
- **Returns**: `IWorkerBinder[T]`, used to bind this worker to various queue types.

**Example:**

```go
import (
    "fmt"
    "github.com/goptics/varmq"
    "time"
)

func myTask(job varmq.Job[string]) {
    fmt.Printf("Processing job %s with data: %s\n", job.ID(), job.Data())
    // Simulate work
    time.Sleep(100 * time.Millisecond)
    fmt.Printf("Finished job %s\n", job.ID())
}

// Create a worker with default concurrency (1)
worker := varmq.NewWorker(myTask)

// Bind to a standard queue
queue := worker.BindQueue()

// Add a job (fire-and-forget style for this worker type when using standard queue)
if job, ok := queue.Add("hello from NewWorker"); ok {
    fmt.Printf("Added job %s to standard queue.\n", job.ID())
    // For NewWorker with standard queues, job.Wait() can be used if needed.
    job.Wait() // Wait for this specific job to complete (optional)
}
```

> NOTE: One worker can be bound with only one queue type at a time. Multi binding is not supported. Will be supported in the near future.

#### Persistent Queue

```go
// Example with persistent queue (conceptual - requires an adapter)
// persistentAdapter := myRedisQueueAdapter.NewQueue("my_jobs")
persistentQ := worker.WithPersistentQueue(persistentAdapter)
if ok := persistentQ.Add("critical task data"); ok {
    fmt.Println("Job added to persistent queue.")
}

worker.WaitUntilFinished() // Wait for all current jobs to finish
```

For concrete examples of persistent queue adapter usage, see the [redis-persistent example](../examples/redis-persistent) and the [sqlite-persistent example](../examples/sqlite-persistent).

#### Distributed Queue

```go
// Example with distributed queue (conceptual - requires an adapter)
distributedQ := worker.WithDistributedQueue(distributedAdapter)
if ok := distributedQ.Add("critical task data"); ok {
    fmt.Println("Job added to distributed queue.")
}

worker.WaitUntilFinished() // Wait for all current jobs to finish
```

For concrete examples of distributed queue adapter usage, see the [redis-distributed example](../examples/redis-distributed)

### 2. `NewErrWorker`

Creates a worker for tasks that only need to report success or failure (an error).

```go
func NewErrWorker[T any](wf func(j Job[T]) error, config ...any) IErrWorkerBinder[T]
```

- **`wf func(j Job[T]) error`**: Your worker function that returns an `error`.
- **Returns**: `IErrWorkerBinder[T]`.
- **Note**: Cannot be bound to persistent or distributed queues.

**Example:**

```go
import (
    "fmt"
    "github.com/goptics/varmq"
)

func validationTask(job varmq.Job[int]) error {
    data := job.Data()
    if data < 0 {
        return fmt.Errorf("job %s: invalid data %d, must be non-negative", job.ID(), data)
    }
    fmt.Printf("Job %s: data %d is valid.\n", job.ID(), data)
    return nil
}

errWorker := varmq.NewErrWorker(validationTask, varmq.WithConcurrency(2))
errQueue := errWorker.BindQueue()

if enqueuedJob, ok := errQueue.Add(42); ok {
    fmt.Printf("Added job %s. Waiting for error status...\n", enqueuedJob.ID())
    if err := enqueuedJob.Err(); err != nil {
        fmt.Printf("Job %s failed: %v\n", enqueuedJob.ID(), err)
    } else {
        fmt.Printf("Job %s succeeded.\n", enqueuedJob.ID())
    }
}

errWorker.WaitUntilFinished() // Wait for all current jobs to finish
```

### 3. `NewResultWorker`

Creates a worker for tasks that produce a result value and may also return an error.

```go
func NewResultWorker[T, R any](wf func(j Job[T]) (R, error), config ...any) IResultWorkerBinder[T, R]
```

- **`wf func(j Job[T]) (R, error)`**: Your worker function returning a result of type `R` and an `error`.
- **Returns**: `IResultWorkerBinder[T, R]`.
- **Note**: Cannot be bound to persistent or distributed queues.

**Example:**

```go
import (
    "fmt"
    "strings"
    "github.com/goptics/varmq"
)

func stringLengthTask(job varmq.Job[string]) (int, error) {
    data := job.Data()
    if data == "" {
        return 0, fmt.Errorf("job %s: input string cannot be empty", job.ID())
    }
    fmt.Printf("Job %s: calculating length of '%s'\n", job.ID(), data)
    return len(data), nil
}

resultWorker := varmq.NewResultWorker(stringLengthTask)
resultQueue := resultWorker.BindQueue()

if enqueuedJob, ok := resultQueue.Add("hello gophers"); ok {
    fmt.Printf("Added job %s. Waiting for result...\n", enqueuedJob.ID())
    res, err := enqueuedJob.Result() // or call Drain() to release resources
    if err != nil {
        fmt.Printf("Job %s failed: %v\n", enqueuedJob.ID(), err)
    } else {
        fmt.Printf("Job %s result: %d\n", enqueuedJob.ID(), res)
    }
}

resultWorker.WaitUntilFinished() // Wait for all current jobs to finish
```

## Worker Configuration

Worker behavior can be customized using configuration functions passed during creation. You can pass an `int` directly for concurrency or use `ConfigFunc` options.

```go
// Example: Concurrency as int
worker1 := varmq.NewWorker(myTask, 4) // 4 concurrent workers

// Example: Using ConfigFunc
worker2 := varmq.NewWorker(myTask, varmq.WithConcurrency(8))

// Example: Multiple ConfigFuncs
worker3 := varmq.NewWorker(myTask,
    varmq.WithConcurrency(2),
    varmq.WithJobIdGenerator(func() string { return "my-prefix-" + uuid.NewString() }),
    varmq.WithMinIdleWorkerRatio(50),
)

// Note: Some configuration options work best together. For example,
// WithMinIdleWorkerRatio is most effective when combined with WithIdleWorkerExpiryDuration.
```

Available `ConfigFunc` options (defined in `config.go`):

| Option                                          | Description                                                                                           | Default                                                                                             |
| ----------------------------------------------- | ----------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `WithConcurrency(n int)`                        | Sets max concurrent worker goroutines. If `n < 1`, uses `runtime.NumCPU()`.                           | `1`                                                                                                 |
| `WithJobIdGenerator(fn func() string)`          | Provides a custom function to generate job IDs.                                                       | Returns `""` (empty string)                                                                         |
| `WithIdleWorkerExpiryDuration(d time.Duration)` | Duration after which excess idle workers (beyond min ratio) are removed.                              | `0` (no expiry, keeps 1 idle)                                                                       |
| `WithMinIdleWorkerRatio(p uint8)`               | Minimum percentage (1-100) of idle workers to maintain relative to max concurrency. Clamped to 1-100. | `1` (effectively keeps 1 idle worker by default, or more if `IdleWorkerExpiryDuration` is also set) |

## The `Worker` Interface

All worker binders (`IWorkerBinder`, `IErrWorkerBinder`, `IResultWorkerBinder`) embed the `Worker` interface, providing methods to control and inspect the worker pool. These methods can typically be accessed via `queue.Worker()`.

```go
// Accessing worker methods through a queue
queue := worker.BindQueue()
workerInstance := queue.Worker()
workerInstance.Pause()
```

Key methods of the `Worker` interface (defined in `worker.go`):

- **Status Inspection:**

  - `IsPaused() bool`: Returns `true` if the worker pool is paused.
  - `IsStopped() bool`: Returns `true` if the worker pool is stopped.
  - `IsRunning() bool`: Returns `true` if the worker pool is active (not paused or stopped).
  - `Status() string`: Returns the current status (e.g., "Initiated", "Running", "Paused", "Stopped").
  - `NumProcessing() int`: Number of jobs currently being processed.
  - `NumConcurrency() int`: Current maximum concurrency (pool size).
  - `NumIdleWorkers() int`: Number of idle workers in the pool.

- **Control Methods:**
  - `TunePool(concurrency int) error`: Dynamically adjusts the worker pool size. Returns error if not running or if concurrency is the same.
  - `Pause() error`: Pauses the worker pool; new jobs won't be picked up. Processing jobs complete.
  - `PauseAndWait() error`: Pauses and waits for all currently processing jobs to finish.
  - `Resume() error`: Resumes a paused worker pool.
  - `Stop() error`: Stops the worker pool gracefully. Waits for processing jobs to finish. Queue is not closed.
  - `Restart() error`: Restarts a stopped worker pool.
  - `WaitUntilFinished()`: Blocks until all jobs _currently in the queue and being processed_ are finished. Does not prevent new jobs from being added.
  - `WaitAndStop() error`: Waits for all jobs in the queue to be processed, then stops the worker.

## Job Lifecycle and Interfaces

Jobs in VarMQ progress through several states: "Created", "Queued", "Processing", "Finished", "Closed".

### Core Job Interfaces (`job.go`)

- **`Job[T any]`**: Basic job information.

  - `ID() string`: The job's unique identifier.
  - `Data() T`: The payload/data of the job.

- **`EnqueuedJob`**: Represents a job successfully added to a queue (typically from `NewWorker` + standard/priority queue).

  - Embeds `Identifiable`, `StatusProvider`, `Awaitable`.
  - `ID() string`
  - `Status() string`: Current lifecycle status.
  - `IsClosed() bool`
  - `Wait()`: Blocks until this job completes processing.
  - `Close() error`: Marks the job as closed; workers will skip it. (Does not remove from queue instantly).

- **`EnqueuedErrJob`**: For jobs from `NewErrWorker`.

  - Embeds `EnqueuedJob` and `Drainer`.
  - `Err() error`: Blocks until completion, then returns the error status.
  - `Drain()`: Cleans up resources (e.g., internal channels). Call if you need to abandon the job without reading its error.

- **`EnqueuedResultJob[R any]`**: For jobs from `NewResultWorker`.
  - Embeds `EnqueuedJob` and `Drainer`.
  - `Result() (R, error)`: Blocks until completion, then returns the result and error.
  - `Drain()`: Cleans up resources. Call if you need to abandon the job without reading its result.

### `Result[T any]` Struct

Used by `EnqueuedResultGroupJob` to stream results.

```go
type Result[T any] struct {
    JobId string
    Data  T
    Err   error
}
```

## Job Configuration

When adding jobs, you can provide optional configurations.

```go
// Example: Custom Job ID
jobId := "my-unique-job-123"
queue.Add(myData, varmq.WithJobId(jobId))
```

Available `JobConfigFunc` options (defined in `config.go`):

| Option                 | Description                                                                                                             |
| ---------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `WithJobId(id string)` | Sets a specific ID for the job. If `id` is empty, it's ignored. If not provided, the worker's `JobIdGenerator` is used. |

## Queue Types and Binding

Workers are bound to queues. The type of worker determines which queue types it can bind to.

### Worker Binder Interfaces (`worker_binder.go`)

These interfaces are returned by the worker creation functions and provide methods to bind to queues.

- **`IWorkerBinder[T]`** (from `NewWorker`)
- **`IErrWorkerBinder[T]`** (from `NewErrWorker`)
- **`IResultWorkerBinder[T, R]`** (from `NewResultWorker`)

### Common Binding Methods

Each binder provides methods to bind to standard and priority queues:

- **Standard Queue (FIFO):**

  - `BindQueue() Queue[T]`: Creates and binds to a new default standard queue.
  - `WithQueue(q IQueue) Queue[T]`: Binds to a custom `IQueue` implementation.
    (Returns `ErrQueue[T]` or `ResultQueue[T,R]` for respective binders).

- **Priority Queue:**
  - `BindPriorityQueue() PriorityQueue[T]`: Creates and binds to a new default priority queue.
  - `WithPriorityQueue(pq IPriorityQueue) PriorityQueue[T]`: Binds to a custom `IPriorityQueue`.
    (Returns `ErrPriorityQueue[T]` or `ResultPriorityQueue[T,R]` for respective binders).

### Specialized Bindings for `IWorkerBinder[T]` (from `NewWorker`)

`NewWorker` is unique in its ability to bind to persistent and distributed queues.

- **Persistent Queues:** (Requires an external `IPersistentQueue` adapter, e.g., Redis-backed)

  - `WithPersistentQueue(pq IPersistentQueue) PersistentQueue[T]`
  - `WithPersistentPriorityQueue(pq IPersistentPriorityQueue) PersistentPriorityQueue[T]`

- **Distributed Queues:** (Requires an external `IDistributedQueue` adapter, e.g., Redis Pub/Sub)
  - `WithDistributedQueue(dq IDistributedQueue) DistributedQueue[T]`
  - `WithDistributedPriorityQueue(dq IDistributedPriorityQueue) DistributedPriorityQueue[T]`
    _Note: When using distributed queues, the worker binder subscribes to queue events (e.g., "enqueued") to trigger job processing._

### Queue Interfaces

All specific queue interfaces (e.g., `Queue[T]`, `PriorityQueue[T]`) embed `IExternalBaseQueue`.

- **`IExternalBaseQueue`** (`queue.go`):

  - `NumPending() int`: Number of jobs waiting in the queue.
  - `Purge()`: Removes all pending jobs from the queue.
  - `Close() error`: Stops the associated worker and closes the queue. Pending jobs may be discarded.
  - `Worker() Worker`: Returns the worker instance bound to this queue.

- **Standard Queues (`queue.go`):**

  - `Queue[T]`: `Add(data T, ...) (EnqueuedJob, bool)`
  - `ErrQueue[T]`: `Add(data T, ...) (EnqueuedErrJob, bool)`
  - `ResultQueue[T, R]`: `Add(data T, ...) (EnqueuedResultJob[R], bool)`
    _Also include `AddAll` methods for each._

- **Priority Queues (`priority.go`):**

  - `PriorityQueue[T]`: `Add(data T, priority int, ...) (EnqueuedJob, bool)`
  - `ErrPriorityQueue[T]`: `Add(data T, priority int, ...) (EnqueuedErrJob, bool)`
  - `ResultPriorityQueue[T, R]`: `Add(data T, priority int, ...) (EnqueuedResultJob[R], bool)`
    _Also include `AddAll` methods for each._

- **Persistent Queues (`persistent.go`, `persistent_priority.go`) - For `NewWorker` only:**

  - `PersistentQueue[T]`: `Add(data T, ...) bool`. Returns `bool` indicating success/failure of enqueueing the serialized job.
  - `PersistentPriorityQueue[T]`: `Add(data T, priority int, ...) bool`.
    _Note: These `Add` methods return `bool` because the job is serialized and handed off. Direct `EnqueuedJob` tracking is not provided from `Add`._

- **Distributed Queues (`distributed.go`, `distributed_priority.go`) - For `NewWorker` only:**
  - `DistributedQueue[T]`: `Add(data T, ...) bool`.
  - `DistributedPriorityQueue[T]`: `Add(data T, priority int, ...) bool`.
    _Similar to persistent queues, `Add` returns `bool`._

## Adding Jobs

### Single Jobs

- **Standard/Priority Queues (Non-Persistent/Distributed):**
  The `Add` method returns an `EnqueuedJob`, `EnqueuedErrJob`, or `EnqueuedResultJob[R]` (and a `bool` for success).

  ```go
  // For Queue[T] from NewWorker
  if enqueuedJob, ok := queue.Add(myData); ok {
    enqueuedJob.Wait()
  }

  // For ErrQueue[T] from NewErrWorker
  if enqueuedErrJob, ok := errQueue.Add(myData); ok {
    // Example: Consume the error. Calling Err() consumes the job's outcome.
    if err := enqueuedErrJob.Err(); err != nil {
       fmt.Printf("Job %s processing error: %v\n", enqueuedErrJob.ID(), err)
    }
    // Note: Calling Err() automatically handles resource cleanup.
    // Only call Drain() if you need to abandon the job without reading its result.
  }

  // For ResultQueue[T,R] from NewResultWorker
  if enqueuedResultJob, ok := resultQueue.Add(myData); ok {
    // Example: Consume the result. Calling Result() consumes the job's outcome.
    if res, err := enqueuedResultJob.Result(); err != nil {
      fmt.Printf("Job %s processing error: %v\n", enqueuedResultJob.ID(), err)
    } else {
      fmt.Printf("Job %s result: %v\n", enqueuedResultJob.ID(), res)
    }
    // Note: Calling Result() automatically handles resource cleanup.
    // Only call Drain() if you need to abandon the job without reading its result.
  }
  ```

- **Persistent/Distributed Queues (Bound with `NewWorker`):**
  The `Add` method returns a `bool`.

  ```go
  // For PersistentQueue[T]
  if ok := persistentQueue.Add(criticalData); ok {
    fmt.Println("Job added to persistent queue.")
  }
  ```

### Batch Jobs (`AddAll`)

Use `AddAll` to enqueue multiple jobs at once. It takes a slice of `varmq.Item[T]`.

```go
type Item[T any] struct {
    ID       string // Optional: custom job ID
    Data     T      // Job payload
    Priority int    // Used by priority queues; ignored by standard queues
}
```

`AddAll` returns a group job handler:

- `EnqueuedGroupJob` (for `Queue[T]` from `NewWorker`):

  - `NumPending() int`
  - `Wait()`: Waits for all jobs in the group to complete.

- `EnqueuedErrGroupJob` (for `ErrQueue[T]` from `NewErrWorker`):

  - `NumPending() int`
  - `Wait()`
  - `Errs() <-chan error`: Returns a channel to receive errors from jobs in the group. Iterate until the channel is closed.
  - `Drain()`: Call if you need to abandon the job without reading its errors.

- `EnqueuedResultGroupJob[R]` (for `ResultQueue[T,R]` from `NewResultWorker`):
  - `NumPending() int`
  - `Wait()`
  - `Results() <-chan Result[R]`: Returns a channel to receive `Result[R]` structs. Iterate until the channel is closed.
  - `Drain()`: Call if you need to abandon the job without reading its result.

**Example with `AddAll` and `EnqueuedResultGroupJob`:**

```go
items := []varmq.Item[string]{
    {Data: "task1", Priority: 1},
    {ID: "custom-id-2", Data: "task2", Priority: 0}, // Higher priority
    {Data: "task3"},
}

// Assuming resultPriorityQueue is a ResultPriorityQueue[string, int]
groupJob := resultPriorityQueue.AddAll(items)

fmt.Printf("%d jobs pending in group.\n", groupJob.NumPending())

// Iterate over the results channel in real-time
for res := range groupJob.Results() {
    if res.Err != nil {
        fmt.Printf("Group job %s failed: %v\n", res.JobId, res.Err)
    } else {
        fmt.Printf("Group job %s result: %v\n", res.JobId, res.Data)
    }
}

// Note: Calling Results() automatically handles resource cleanup.
// Only call Drain() if you need to abandon the job without reading its result.
fmt.Println("All group jobs processed.")
```
