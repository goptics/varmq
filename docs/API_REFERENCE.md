# VarMQ API Reference

Comprehensive API documentation for theVarMQ (Go Concurrent Queue) library.

## Worker Creation

VarMQ provides three main worker creation functions, each designed for different use cases.

### `NewWorker`

Creates a worker that processes items and returns both a result and an error.

```go
func NewWorker[T, R any](wf WorkerFunc[T, R], config ...any) IWorkerBinder[T, R]
```

**Example:**

```go
worker := varmq.NewWorker(func(data string) (int, error) {
    return len(data), nil
})

queue := worker.BindQueue()

data, err := queue.Add("hello gophers").Result()
if err != nil {
    fmt.Printf("Error adding job: %v\n", err)
    return
}

fmt.Printf("Result: %d\n", data)
```

### `NewErrWorker`

Creates a worker for operations that only need to return an error status (no result value).

```go
func NewErrWorker[T any](wf WorkerErrFunc[T], config ...any) IWorkerBinder[T, any]
```

**Example:**

```go
worker := varmq.NewErrWorker(func(data int) error {
    log.Printf("Processing: %d", data)
    return nil
})

queue := worker.BindQueue()
queue.Add(42).Drain() // Returns a job that will only indicate success/failure
```

### `NewVoidWorker`

Creates a worker for operations that don't return any value (void functions). This is the most performant worker type and the only one that can be bound to distributed queues.

```go
func NewVoidWorker[T any](wf VoidWorkerFunc[T], config ...any) IVoidWorkerBinder[T]
```

**Example:**

```go
worker := varmq.NewVoidWorker(func(data int) {
    fmt.Printf("Processing: %d\n", data)
})

q1 := worker.BindQueue()
q1.Add(42).Drain() // Fire and forget

q2 := worker.BindPriorityQueue() // ❌ one worker can't be bound with multiple queues. it will panic

q2 := worker.Copy().BindPriorityQueue() // ✅ using Copy, you can bind multiple queues but each queue will have its own worker
```

### Worker Configuration

All worker creation functions accept optional configuration parameters that customize worker behavior. These can be passed as additional arguments after the worker function.

```go
// Create a worker with 8 concurrent processors
worker := varmq.NewWorker(myWorkerFunc, varmq.WithConcurrency(8))

// Or simply pass an integer for concurrency (shorthand)
worker := varmq.NewWorker(myWorkerFunc, 8)


// Multiple configurations can be combined
worker := varmq.NewWorker(myWorkerFunc,
    varmq.WithConcurrency(8),
    varmq.WithJobIdGenerator(myIdGenerator))
```

#### Configuration Options

| Configuration                            | Description                                                         | Default                       |
| ---------------------------------------- | ------------------------------------------------------------------- | ----------------------------- |
| `WithConcurrency(n)`                     | Sets the number of concurrent workers                               | `1`                           |
| `WithCache(cache)`                       | Provides a custom cache implementation                              | In-memory cache               |
| `WithAutoCleanupCache(duration)`         | Sets the cache cleanup interval                                     | No auto-cleanup               |
| `WithJobIdGenerator(func)`               | Custom job ID generation function                                   | Empty string (auto-generated) |
| `WithIdleWorkerExpiryDuration(duration)` | Sets how long idle workers will be kept before expiry               | `0` (no expiry)               |
| `WithMinIdleWorkerRatio(percentage)`     | Sets the percentage of idle workers to keep relative to concurrency | `0` (no minimum)              |

**Examples:**

```go
// Set concurrency to use all available CPU cores
// if the concurrency is set to less than 1, then its set the concurrency number of cpu using runtime.NumCPU() func
worker := varmq.NewWorker(myFunc, varmq.WithConcurrency(0))

// Combine idle worker management options with other configuration
worker := varmq.NewWorker(myFunc,
    varmq.WithConcurrency(50), // Setting the max concurrency to 50
    varmq.WithIdleWorkerExpiryDuration(5 * time.Minute), // Set idle worker expiry duration to 5 minutes
    varmq.WithMinIdleWorkerRatio(20), // This will keep 10 idle workers (20% of 50) and expire others (40) after 5 minutes of inactivity
    )

// Use custom job ID generator
worker := varmq.NewWorker(myFunc, varmq.WithJobIdGenerator(func() string {
    return uuid.New().String() // Using UUID for job IDs
}))

// Configure cache to clean up every hour
worker := varmq.NewWorker(myFunc, varmq.WithAutoCleanupCache(1 * time.Hour))
```

## Queue Types

VarMQ supports different queue types for various use cases.

### Standard Queue

A First-In-First-Out (FIFO) queue for sequential processing of jobs.

```go
// Create and bind a standard queue
queue := worker.BindQueue()

// Or use a custom queue implementation
customQueue := myCustomQueue // implements IQueue
queue := worker.WithQueue(customQueue)
```

### Priority Queue

Processes jobs based on their assigned priority rather than insertion order.

```go
// Create and bind a priority queue
priorityQueue := worker.BindPriorityQueue()

// Add a job with priority (lower numbers = higher priority)
priorityQueue.Add(data, 5).Drain()

// Or use a custom priority queue implementation
customPriorityQueue := myCustomPriorityQueue // implements IPriorityQueue
priorityQueue := worker.WithPriorityQueue(customPriorityQueue)
```

### Persistent Queue

Ensures jobs are not lost even if the application crashes or restarts.

```go
// Bind to a persistent queue implementation
persistentQueue := myPersistentQueue // implements IPersistentQueue
queue := worker.WithPersistentQueue(persistentQueue)
```

**SQLite Adapter Example:**

```go
package main

import (
    "fmt"
    "time"

    "github.com/goptics/sqliteq"
    "github.com/goptics/varmq"
    "github.com/lucsky/cuid"
)

func main() {
    w := varmq.NewVoidWorker(func(num int) {
        println(num)
        time.Sleep(1 * time.Second)
    }, 2)
    sq := sqliteq.New("test.db")
    pq, err := sq.NewQueue("test")

    if err != nil {
        fmt.Println(err)
        return
    }

    q := w.WithPersistentQueue(pq)
    defer q.WaitUntilFinished()

    for i := range 20 {
        q.Add(i, varmq.WithJobId(cuid.New()))
    }

    fmt.Println("done")
}
```

**Redis Adapter Example:**

```go
// Using the redisq adapter (one of many possible adapters)
import (
    "github.com/goptics/varmq"
    "github.com/goptics/redisq"
)

// Connect to Redis using the adapter
redisQueue := redisq.New("redis://localhost:6379")
defer redisQueue.Close()

// Create a persistent queue
persistentQueue := redisQueue.NewQueue("my_jobs")
defer persistentQueue.Close()

// Create a worker and bind to the persistent queue
worker := varmq.NewWorker(func(data string) (string, error) {
    return "Processed: " + data, nil
}, 5)

// Bind the worker to the persistent queue
queue := worker.WithPersistentQueue(persistentQueue)
```

**Creating Your Own Adapter:**

You can create your own persistent queue adapter by implementing the `IPersistentQueue` interface:

```go
// IPersistentQueue is the root interface of persistent queue operations.
type IPersistentQueue interface {
    IQueue
    IAcknowledgeable
}

// IAcknowledgeable is the root interface of acknowledgeable operations.
type IAcknowledgeable interface {
    // Returns true if the item was successfully acknowledged, false otherwise.
    Acknowledge(ackID string) bool
    // DequeueWithAckId dequeues an item from the queue
    // Returns the item, a boolean indicating if the operation was successful, and the acknowledgment ID.
    DequeueWithAckId() (any, bool, string)
}
```

### Persistent Priority Queue

Combines persistence with priority-based processing.

```go
// Bind to a persistent priority queue implementation
persistentPriorityQueue := myPersistentPriorityQueue // implements IPersistentPriorityQueue
queue := worker.WithPersistentPriorityQueue(persistentPriorityQueue)
```

### Distributed Queue

Allows job processing across multiple instances or processes. Only compatible with void workers.

```go
// Bind to a distributed queue implementation
distributedQueue := myDistributedQueue // implements IDistributedQueue
queue := voidWorker.WithDistributedQueue(distributedQueue)
```

**Redis Adapter for Distributed Queue:**

```go
// Provider (adds jobs to queue)
import (
    "fmt"
    "github.com/goptics/varmq"
    "github.com/goptics/redisq"
)

// Connect to Redis ensure the redis server is running
redisQueue := redisq.New("redis://localhost:6379")
rq := redisQueue.NewDistributedQueue("jobs_queue")

// Create a distributed queue
distQueue := varmq.NewDistributedQueue[string, string](rq)

// Add jobs from anywhere
for i := 0; i < 1000; i++ {
    distQueue.Add(fmt.Sprintf("Job %d", i))
}
```

```go
// Consumer (processes jobs)
import (
    "fmt"
    "github.com/goptics/varmq"
    "github.com/goptics/redisq"
)

// Connect to the same Redis server
redisQueue := redisq.New("redis://localhost:6379")
rq := redisQueue.NewDistributedQueue("jobs_queue")

// Create a worker
worker := varmq.NewVoidWorker(func(data string) {
    fmt.Println("Processing:", data)
}, 10) // 10 concurrent workers

// Bind to distributed queue
queue := worker.WithDistributedQueue(rq)

// Start listening for jobs
rq.Listen()
```

**Creating Your Own Distributed Queue Adapter:**

You can create your own distributed queue adapter by implementing the `IDistributedQueue` interface:

```go
// IDistributedQueue is the root interface of distributed queue operations.
type IDistributedQueue interface {
    IPersistentQueue
    ISubscribable
}

// ISubscribable is the root interface of subscribable operations.
type ISubscribable interface {
    Subscribe(func(action string))
}
```

### Distributed Priority Queue

Combines distributed processing with priority-based ordering.

```go
// Bind to a distributed priority queue implementation
distributedPriorityQueue := myDistributedPriorityQueue // implements IDistributedPriorityQueue
queue := voidWorker.WithDistributedPriorityQueue(distributedPriorityQueue)
```

## Queue Operations

### Adding Jobs

```go
// Add a single job
job := queue.Add(data)

// Add multiple jobs
groupJob := queue.AddAll([]varmq.Item{
    {ID: "job1", Value: data1},
    {ID: "job2", Value: data2},
})

// If you don't need the results, use Drain to free the result channel resources
job.Drain()

> For group jobs, call `groupJob.Drain()` to free the shared result channel
> Note: Individual jobs in a group job are not accessible - you can only drain the entire group
```

```go
// Create a worker that processes strings and returns their length
worker := varmq.NewWorker(func(data string) (int, error) {
    return len(data), nil
}, 4) // 4 concurrent workers

// Bind a queue
queue := worker.BindQueue()

// Create a batch of items to process
items := []varmq.Item[string]{
    {ID: "job1", Value: "hello"},
    {ID: "job2", Value: "world"},
    {ID: "job3", Value: "concurrent"},
}

// Add all items to the queue at once
groupJob := queue.AddAll(items)

// Stream all results through a non-blocking channel
resultsChan, err := groupJob.Results()
if err != nil {
    fmt.Printf("Error getting results channel: %v\n", err)
    return
}

// Process results as they arrive even though they are processed concurrently
for result := range resultsChan {
    // Each result contains JobId, Data, and Err fields
    if result.Err != nil {
        fmt.Printf("Job %s failed with error: %v\n", result.JobId, result.Err)
    } else {
        fmt.Printf("Job %s result: %v\n", result.JobId, result.Data)
    }
}
```

### Shutdown Operations

```go
// Graceful shutdown - waits for all jobs to complete
queue.WaitAndClose()

// Immediate shutdown - discards pending jobs
queue.Close()

// Purge - removes all pending jobs without shutting down
queue.Purge()
```

## Worker Control

VarMQ provides several methods to control worker behavior at runtime. Most control methods affect the worker's status which can be checked using `worker.Status()`.

### `Pause()`

Temporarily suspends worker processing. Jobs will remain in the queue but won't be processed until resumed.

- Sets worker status to `"Paused"`
- All currently running jobs will complete, but no new jobs will be processed

```go
worker.Pause()
fmt.Println(worker.Status()) // Outputs: "Paused"
```

### `Resume()`

Resumes processing of jobs after the worker has been paused.

- Sets worker status to `"Running"`
- Begins processing jobs from the queue again
- Returns an error if the worker is already running

```go
err := worker.Resume()
fmt.Println(worker.Status()) // Outputs: "Running"
```

### `Stop()`

Shuts down the worker pool by removing all running goroutines. The worker can be restarted later using the `Restart()` method.

- Sets worker status to `"Stopped"`
- Stops all background processes (timers, tickers)
- Closes job channels and clears the channel stack
- Clears the worker cache

```go
worker.Stop()
fmt.Println(worker.Status()) // Outputs: "Stopped"
```

### `Restart()`

Reinitializes worker goroutines. Useful after configuration changes or to recover from errors.

- First pauses and waits for ongoing jobs to complete
- Reinitializes the worker's internal notifier system
- Sets worker status to `"Running"`
- Returns any error that occurs during restart

```go
err := worker.Restart()
fmt.Println(worker.Status()) // Outputs: "Running"
```

### `TunePool(concurrency int) error`

Dynamically adjusts the number of concurrent worker goroutines at runtime.

- Returns an error if the worker is not in the running state
- Does not affect the worker's status
- Only changes the number of concurrent worker goroutines

```go
// Later, scale up concurrency based on load
worker.TunePool(8)  // Scale up to 8 workers

// Later, scale down when load decreases
worker.TunePool(2)  // Scale down to 2 workers
```

## Worker Status Methods

VarMQ provides methods to query the current status and state of workers. These methods are useful for monitoring, logging, and implementing adaptive behavior based on the worker's current state.

### `IsPaused() bool`

Checks if the worker is currently in the paused state.

```go
if worker.IsPaused() {
    fmt.Println("Worker is paused")
}
```

### `IsStopped() bool`

Checks if the worker is currently in the stopped state.

```go
if worker.IsStopped() {
    fmt.Println("Worker is stopped")
}
```

### `IsRunning() bool`

Checks if the worker is currently in the running state and processing jobs.

```go
if worker.IsRunning() {
    fmt.Println("Worker is actively processing jobs")
}
```

### `Status() string`

Returns the current status of the worker as a string. Possible values are "Initiated", "Running", "Paused", and "Stopped".

```go
status := worker.Status()
fmt.Println("Current worker status:", status)  // e.g., "Current worker status: Running"
```

### `NumProcessing() int`

Returns the number of jobs currently being processed by the worker. This can be useful for monitoring workload and implementing adaptive behavior.

```go
processingCount := worker.NumProcessing()
fmt.Printf("Currently processing %d jobs\n", processingCount)
```

#### `NumConcurrency() int`

Returns the current max concurrency. This indicates how many jobs the worker can process simultaneously.

```go
concurrency := worker.NumConcurrency()
fmt.Printf("Worker is configured with %d concurrent processors\n", concurrency)
```

#### `NumIdleWorkers() int`

Returns the number of idle workers currently in the pool. This can be useful for monitoring resource usage and understanding the effects of your idle worker configuration.

```go
idleWorkers := worker.NumIdleWorkers()
fmt.Printf("Worker has %d idle workers ready to process jobs\n", idleWorkers)
```

## Adapters

VarMQ supports multiple storage backends through adapters. An adapter is any implementation that satisfies the required interfaces.

### Available Adapters

- **Redis:** [redisq](https://github.com/goptics/redisq) - Redis-based adapter for persistent and distributed queues
- **SQLite:** [sqliteq](https://github.com/goptics/sqliteq) - SQLite-based adapter for persistent queues
- **DuckDB:** [duckdbq](https://github.com/goptics/duckdbq) - DuckDB-based adapter for persistent queues

### Planned Adapters

- **PostgreSQL** - For robust persistent and distributed queues
- **DiceDB** - Future adapter implementation

### Creating Custom Adapters

You can create your own adapters by implementing the appropriate interfaces:

- For persistent queues: `IPersistentQueue`
- For persistent priority queues: `IPersistentPriorityQueue`
- For distributed queues: `IDistributedQueue`
- For distributed priority queues: `IDistributedPriorityQueue`

Example skeleton of a custom adapter:

```go
type MyPersistentQueue struct {
    // Your implementation details
}

// Implement IQueue methods
func (q *MyPersistentQueue) Enqueue(item any) bool {
    // Store the item in your backend
}

// ... implement other required methods

// Implement IAcknowledgeable methods
func (q *MyPersistentQueue) Acknowledge(ackID string) bool {
    // Mark the item as acknowledged in your backend
}

func (q *MyPersistentQueue) DequeueWithAckId() (any, bool, string) {
    // Store the item for future acknowledgment
}
```

## Interface Hierarchy

**Click to Open [VarMQ Interface Hierarchy Diagram](../diagrams/interface.drawio.png)**

## Job Management

### `Job`

Represents a job that can be enqueued and processed, returned by invoking `Add` and `AddAll` method

#### Methods

- `Status() string`

  - Returns the current status of the job.

- `IsClosed() bool`

  - Returns whether the job is closed.

- `Drain()`

  - Discards the job's result and error values asynchronously.

- `Result() (R, error)`

  - Blocks until the job completes and returns the result and any error.

- `Errors() <-chan error`

  - Returns a channel that will receive the errors of the void group job.

- `Results() (<-chan Result[R], error)`
  - Returns a receive-only channel that will receive the results of the group job and an error if one occurred during channel creation.
