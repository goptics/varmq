# GoCQ API Reference

Comprehensive API documentation for the GoCQ (Go Concurrent Queue) library.

## Table of Contents

- [GoCQ API Reference](#gocq-api-reference)
  - [Table of Contents](#table-of-contents)
  - [Worker Creation](#worker-creation)
    - [`NewWorker`](#newworker)
    - [`NewErrWorker`](#newerrworker)
    - [`NewVoidWorker`](#newvoidworker)
  - [Queue Types](#queue-types)
    - [Standard Queue](#standard-queue)
    - [Priority Queue](#priority-queue)
    - [Persistent Queue](#persistent-queue)
    - [Persistent Priority Queue](#persistent-priority-queue)
    - [Distributed Queue](#distributed-queue)
    - [Distributed Priority Queue](#distributed-priority-queue)
  - [Queue Operations](#queue-operations)
    - [Adding Jobs](#adding-jobs)
    - [Waiting for Jobs](#waiting-for-jobs)
    - [Shutdown Operations](#shutdown-operations)
  - [Worker Control](#worker-control)
  - [Interface Hierarchy](#interface-hierarchy)
  - [Job Management](#job-management)
    - [`Job`](#job)

## Worker Creation

GoCQ provides three main worker creation functions, each designed for different use cases.

### `NewWorker`

Creates a worker that processes items and returns both a result and an error.

```go
func NewWorker[T, R any](wf WorkerFunc[T, R], config ...any) IWorkerBinder[T, R]
```

**Example:**

```go
worker := gocq.NewWorker(func(data string) (int, error) {
    return len(data), nil
}, 4) // 4 concurrent workers

queue := worker.BindQueue()
queue.Add("hello") // Returns a job that will produce an int result
```

### `NewErrWorker`

Creates a worker for operations that only need to return an error status (no result value).

```go
func NewErrWorker[T any](wf WorkerErrFunc[T], config ...any) IWorkerBinder[T, any]
```

**Example:**

```go
worker := gocq.NewErrWorker(func(data int) error {
    log.Printf("Processing: %d", data)
    return nil
})

queue := worker.BindQueue()
queue.Add(42) // Returns a job that will only indicate success/failure
```

### `NewVoidWorker`

Creates a worker for operations that don't return any value (void functions). This is the most performant worker type and the only one that can be bound to distributed queues.

```go
func NewVoidWorker[T any](wf VoidWorkerFunc[T], config ...any) IVoidWorkerBinder[T]
```

**Example:**

```go
worker := gocq.NewVoidWorker(func(data int) {
    fmt.Printf("Processing: %d\n", data)
})

queue := worker.BindQueue()
queue.Add(42) // Fire and forget
```

## Queue Types

GoCQ supports different queue types for various use cases.

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
priorityQueue.Add(data, 5)

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
jobs := queue.AddAll([]gocq.Item{
    {ID: "job1", Value: data1},
    {ID: "job2", Value: data2},
})
```

### Waiting for Jobs

```go
// Wait for a single job result
result, err := job.Result()

// Wait for multiple job results
results, errs := jobs.Results()
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

```go
// Pause worker processing
worker.Pause()

// Resume worker processing
worker.Resume()

// Stop worker (terminates all processing)
worker.Stop()
```

## Interface Hierarchy

**Click to Open [GoCQ Interface Hierarchy Diagram](../interface.drawio.png)**

## Job Management

### `Job`

Represents a job that can be enqueued and processed, returned by invoking `Add` and `AddAll` method

**Methods**

- `Status() string`

  - Returns the current status of the job.

- `IsClosed() bool`

  - Returns whether the job is closed.

- `Drain()`

  - Discards the job's result and error values asynchronously.

- `Close() error`

  - Closes the job and its associated channels.

- `Result() (R, error)`

  - Blocks until the job completes and returns the result and any error.

- `Errors() <-chan error`

  - Returns a channel that will receive the errors of the void group job.

- `Results() chan Result[T]`
  - Returns a channel that will receive the results of the group job.
