# GoCQ API Reference

Comprehensive API documentation for the GoCQ (Go Concurrent Queue) library.

## Table of Contents

- [GoCQ API Reference](#gocq-api-reference)
  - [Table of Contents](#table-of-contents)
  - [Standard Queue (FIFO)](#standard-queue-fifo)
    - [`ConcurrentQueue`](#concurrentqueue)
  - [Priority Queue](#priority-queue)
    - [`ConcurrentPriorityQueue`](#concurrentpriorityqueue)
  - [Void Queue](#void-queue)
    - [`ConcurrentVoidQueue`](#concurrentvoidqueue)
  - [Void Priority Queue](#void-priority-queue)
    - [`ConcurrentVoidPriorityQueue`](#concurrentvoidpriorityqueue)
    - [`Job`](#job)

## Standard Queue (FIFO)

### `ConcurrentQueue`

A generic concurrent queue with FIFO ordering.

**Methods**

- `NewQueue(concurrency uint32, worker Worker[T, R]) *ConcurrentQueue[T, R]`

  - Creates a new ConcurrentQueue with the specified concurrency and worker function.

- `Pause() IConcurrentQueue[T, R]`

  - Pauses the processing of jobs.

- `Add(data T) EnqueuedJob[R]`

  - Adds a new Job to the queue and returns an EnqueuedJob to handle the job.
  - Time complexity: O(1)

- `AddAll(data []T) EnqueuedGroupJob[R]`

  - Adds multiple Jobs to the queue and returns an EnqueuedGroupJob to handle the job.
  - Time complexity: O(n) where n is the number of Jobs added

- `Restart()`

  - Restarts the queue and initializes the worker goroutines based on the concurrency.
  - Time complexity: O(n) where n is the concurrency

- `Resume()`

  - Continues processing jobs that are pending in the queue.

- `PendingCount() int`

  - Returns the number of Jobs pending in the queue.
  - Time complexity: O(1)

- `CurrentProcessingCount() uint32`

  - Returns the number of Jobs currently being processed.
  - Time complexity: O(1)

- `WaitUntilFinished()`

  - Waits until all pending Jobs in the queue are processed.
  - Time complexity: O(n) where n is the number of pending Jobs

- `Purge()`

  - Removes all pending Jobs from the queue.
  - Time complexity: O(n) where n is the number of pending Jobs

- `WaitAndClose() error`

  - Waits until all pending Jobs in the queue are processed and then closes the queue.
  - Time complexity: O(n) where n is the number of pending Jobs

- `Close() error`
  - Closes the queue and resets all internal states.
  - Time complexity: O(n) where n is the number of channels

## Priority Queue

### `ConcurrentPriorityQueue`

A generic concurrent priority queue (default FIFO). inherits the methods from `ConcurrentQueue`

**Methods**

- `NewPriorityQueue(concurrency uint32, worker Worker[T, R]) *ConcurrentPriorityQueue[T, R]`

  - Creates a new ConcurrentPriorityQueue with the specified concurrency and worker function.

- `Pause() IConcurrentPriorityQueue[T, R]`

  - Pauses the processing of jobs.

- `Add(data T, priority int) EnqueuedJob[R]`

  - Adds a new Job with the given priority to the queue and returns an EnqueuedJob to handle the job.
  - Time complexity: O(log n)

- `AddAll(items []PQItem[T]) EnqueuedGroupJob[R]`

  - Adds multiple Jobs with the given priority to the queue and returns an EnqueuedGroupJob to handle the job.
  - Time complexity: O(n log n) where n is the number of Jobs added

## Void Queue

### `ConcurrentVoidQueue`

A concurrent queue for void jobs (jobs that do not return a result). It inherits the methods from `ConcurrentQueue` but with void-specific methods.

**Methods**

- `NewQueue(concurrency uint32, worker VoidWorker[T]) *ConcurrentVoidQueue[T]`

  - Creates a new ConcurrentVoidQueue with the specified concurrency and worker function.

- `Pause() IConcurrentVoidQueue[T]`

  - Pauses the processing of jobs.

- `Add(data T) EnqueuedVoidJob`

  - Adds a new Job to the queue and returns an EnqueuedVoidJob to handle the void job.
  - Time complexity: O(1)

- `AddAll(items []T) EnqueuedVoidGroupJob`

  - Adds multiple Jobs to the queue and returns an EnqueuedVoidGroupJob to handle the job.
  - Time complexity: O(n) where n is the number of Jobs added

## Void Priority Queue

### `ConcurrentVoidPriorityQueue`

A concurrent priority queue for void jobs (jobs that do not return a result). inherits the methods from `ConcurrentPriorityQueue`

**Methods**

- `NewPriorityQueue(concurrency uint32, worker VoidWorker[T]) *ConcurrentVoidPriorityQueue[T]`

  - Creates a new ConcurrentVoidPriorityQueue with the specified concurrency and worker function.

- `Pause() IConcurrentVoidPriorityQueue[T]`

  - Pauses the processing of jobs.

- `Add(data T, priority int) EnqueuedVoidJob`

  - Adds a new Job with the given priority to the queue and returns an EnqueuedVoidJob to handle the job.
  - Time complexity: O(log n)

- `AddAll(items []PQItem[T]) EnqueuedVoidGroupJob`

  - Adds multiple Jobs with the given priority to the queue and returns an EnqueuedVoidGroupJob to handle the job.
  - Time complexity: O(n log n) where n is the number of Jobs added

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

- `WaitForResult() (R, error)`

  - Blocks until the job completes and returns the result and any error.

- `WaitForError() error`

  - Blocks until an error is received on the error channel.

- `Errors() <-chan error`

  - Returns a channel that will receive the errors of the void group job.

- `Results() chan shared.Result[T]`
  - Returns a channel that will receive the results of the group job.
