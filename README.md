# GoCQ: High-Performance Concurrent Queue for Gophers

Package gocq offers a concurrent queue system using channels and goroutines, supporting both FIFO and priority operations, with options for result-returning and void (non-returning) queues.

[![Go Reference](https://img.shields.io/badge/go-pkg-00ADD8.svg?logo=go)](https://pkg.go.dev/github.com/fahimfaisaal/gocq)
[![Go Report Card](https://goreportcard.com/badge/github.com/fahimfaisaal/gocq)](https://goreportcard.com/report/github.com/fahimfaisaal/gocq)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://golang.org/doc/devel/release.html)
[![CI](https://github.com/fahimfaisaal/gocq/actions/workflows/go.yml/badge.svg)](https://github.com/fahimfaisaal/gocq/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/fahimfaisaal/gocq/branch/main/graph/badge.svg)](https://codecov.io/gh/fahimfaisaal/gocq)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)

GoCQ is a high-performance concurrent queue for Go, optimized for efficient task processing. It supports both FIFO and priority queues, featuring non-blocking job submission, dedicated worker channels, and a pre-allocated worker pool to ensure smooth and controlled concurrency. With optimized memory management, GoCQ minimizes allocations and prevents goroutine leaks, making it a reliable choice for high-throughput applications.

## 🌟 Features

- Generic type support for both data and results
- ResultChannel-returning and void (non-returning) queue variants
- Configurable concurrency limits
- FIFO queue with O(1) operations
- Priority queue support with O(log n) operations
- Pause/Resume functionality
- Clean and graceful shutdown mechanisms
- Thread-safe operations
- Non-blocking job submission

## 📋 Table of Contents

- [Installation](#-installation)
- [Quick Start](#-quick-start)
- [API Reference](#-api-reference)
  - [Standard Queue](#standard-queue-fifo)
  - [Void Queue](#void-queue)
  - [Priority Queue](#priority-queue)
  - [Void Priority Queue](#void-priority-queue)
- [Examples](#-examples)
- [Performance](#-performance)

## 🔧 Installation

```bash
go get github.com/fahimfaisaal/gocq
```

## 🚀 Quick Start

### Standard Queue (FIFO and ResultChannel-Returning)

```go
package main

import (
  "fmt"
  "time"
  "github.com/fahimfaisaal/gocq"
)

func main() {
  // Create a queue with 2 concurrent workers
  queue := gocq.NewQueue(2, func(data int) (int, error) {
    time.Sleep(500 * time.Millisecond)
    return data * 2, nil
  })
  defer queue.Close()

  // Add a single job
  result, err := queue.Add(5).Wait()
  fmt.Println(result, err) // Output: 10 nil

  // Add multiple jobs
  for result := range queue.AddAll([]int{1, 2, 3, 4, 5}) {
    if result.Err != nil {
      fmt.Printf("Error: %v\n", result.Err)
      continue
    }
    fmt.Println(result.Data) // Output: 2, 4, 6, 8, 10 (unordered)
  }
}
```

### Void Queue (No Return Value)

```go
package main

import (
  "fmt"
  "time"
  "github.com/fahimfaisaal/gocq"
)

func main() {
  // Create a void queue with 2 concurrent workers
  queue := gocq.NewVoidQueue(2, func(data int) error {
    time.Sleep(500 * time.Millisecond)
    fmt.Printf("Processed: %d\n", data)
    return nil
  })
  defer queue.WaitAndClose()

  // Add jobs
  if err := <-queue.Add(5); err != nil {
    fmt.Printf("Error: %v\n", err)
  }

  errs := queue.AddAll([]int{1, 2, 3})
  for err := range errs {
    if err != nil {
      fmt.Printf("Error: %v\n", err)
    }
  }
}
```

> Note: Void queue is almost ~25% faster than the standard queue (result returning) according to the benchmarks. Also mem allocations are less in void queue.

## 📚 API Reference

### Standard Queue (FIFO)

#### `NewQueue[T, R any](concurrency uint32, worker func(T) (R, error)) *ConcurrentQueue[T, R]`

Creates a new concurrent FIFO queue.

- Time Complexity: O(c) where c is concurrency and spawns c goroutines
- Parameters:
  - `concurrency`: Maximum number of concurrent workers
  - `worker`: Function to process each job, returns result and error
- Returns: A new concurrent queue instance

#### Queue Operation Methods

#### `Init() *ConcurrentQueue[T, R]`

Initializes the queue and starts worker goroutines.

- Time Complexity: O(c) where c is concurrency
- Returns: Queue instance for chaining
- Effect: Starts worker goroutines and closes old ones

> Note: Closes old channels to prevent routine leaks

#### `Add(data T) <-chan ResultChannel[R]`

Adds a single job to the queue.

- Time Complexity: O(1)
- Returns: ResultChannel to receive the result

#### `AddAll(data []T) <-chan ResultChannel[R]`

Adds multiple jobs to the queue.

- Time Complexity: O(n) where n is number of jobs
- Returns: Merged channel to receive all results

#### `Pause() *ConcurrentQueue[T, R]`

Pauses job processing.

- Time Complexity: O(1)
- Returns: Queue instance for chaining
- Effect: Stops processing next pending jobs

#### `Resume()`

Resumes job processing.

- Time Complexity: O(c) where c is the concurrency
- Effect: Processes next pending jobs up to concurrency limit

#### Cleanup and Wait Methods

#### `Purge()`

Removes all pending jobs from the queue.

- Time Complexity: O(n) where n is number of pending jobs
- Effect: All pending jobs are removed, but currently processing jobs will continue

> Note: Closes response channels for all purged jobs

#### `WaitUntilFinished() error`

Blocks until all pending jobs complete.

- Time Complexity: O(n) where n is number of currently processing and pending jobs
- Effect: Blocks until all pending jobs are processed

#### `Close() error`

Closes the queue and cleans up resources.

- Time Complexity: O(c) where c is concurrency
- Effect: Closes all channels and resets internal state

> Note: Uses `Purge()` to remove pending jobs and then `WaitUntilFinished()` for waiting currently processing jobs internally

#### `WaitAndClose()`

Waits for completion of each pending job and closes the queue. executes `WaitUntilFinished()` and then `Close()`

#### State Management Methods

#### `PendingCount() int`

Returns the number of jobs waiting to be processed.

- Time Complexity: O(1)
- Returns: Number of pending jobs

#### `CurrentProcessingCount() uint32`

Returns the number of jobs currently being processed.

- Time Complexity: O(1)
- Returns: Number of active jobs

#### `IsPaused() bool`

Checks if the queue is currently paused.

- Time Complexity: O(1)
- Returns: true if paused, false otherwise

### Void Queue

#### `NewVoidQueue[T any](concurrency uint32, worker func(T)) *ConcurrentVoidQueue[T]`

Creates a new concurrent FIFO queue for operations without return values.

- Time Complexity: O(c) where c is concurrency
- Parameters:
  - `concurrency`: Maximum number of concurrent workers
  - `worker`: Function to process each job (void return)
- Returns: A new void queue instance

#### Void Queue Operation Methods

Similar to standard queue but without return channels:

- `Add(data T)`: Adds a single job
- `AddAll(data []T)`: Adds multiple jobs
- `Pause()`, `Resume()`, `Close()`, etc. work the same as standard queue

### Priority Queue

**The priority queue extends the standard queue with priority support.**

#### `NewPriorityQueue[T, R any](concurrency uint32, worker func(T) (R, error)) *ConcurrentPriorityQueue[T, R]`

Creates a new concurrent priority queue.

- Time Complexity: O(1)
- Parameters:
  - `concurrency`: Maximum number of concurrent workers
  - `worker`: Function to process each job
- Returns: A new priority queue instance

#### `Add(data T, priority int) <-chan ResultChannel[R]`

Adds a job with priority (lower number = higher priority).

- Time Complexity: O(log n) where n is queue size
- Parameters:
  - `priority`: Lower value means higher priority
- Returns: ResultChannel to receive the result

#### `AddAll(items []PQItem[T]) <-chan ResultChannel[R]`

Adds multiple prioritized jobs.

- Time Complexity: O(n log n) where n is number of items
- Returns: Merged channel to receive all results in priority order

### Void Priority Queue

#### `NewVoidPriorityQueue[T any](concurrency uint32, worker func(T)) *ConcurrentVoidPriorityQueue[T]`

Creates a new concurrent priority queue for operations without return values.

- Time Complexity: O(1)
- Parameters:
  - `concurrency`: Maximum number of concurrent workers
  - `worker`: Function to process each job (void return)
- Returns: A new void priority queue instance

#### Void Priority Queue Operation Methods

Similar to standard priority queue but without return channels:

- `Add(data T, priority int)`: Adds a job with priority
- `AddAll(items []PQItem[T])`: Adds multiple prioritized jobs
- Other methods work the same as standard priority queue

## 💡 Examples

### Priority Queue Example

```go
queue := gocq.NewPriorityQueue(1, func(data int) (int, error) {
    time.Sleep(500 * time.Millisecond)
    return data * 2, nil
})
defer queue.WaitAndClose()

// Add jobs with different priorities
items := []gocq.PQItem[int]{
    {Value: 1, Priority: 2}, // Lowest priority
    {Value: 2, Priority: 1}, // Medium priority
    {Value: 3, Priority: 0}, // Highest priority
}

results := queue.AddAll(items)
for result := range results {
    if result.Err != nil {
        fmt.Printf("Error: %v\n", result.Err)
        continue
    }
    fmt.Println(result.Data) // Output: 6, 4, 2 (processed by priority)
}
```

### Pause/Resume Example

```go
queue := gocq.NewQueue(2, func(data int) int {
    time.Sleep(500 * time.Millisecond)
    return data * 2
}).Pause() // paused

// Add jobs while paused (non-blocking)
resp1 := queue.Add(1)
resp2 := queue.Add(2)

// Resume processing
queue.Resume()

fmt.Println(<-resp1, <-resp2) // Output: 2 4 (unordered due to concurrency)
```

### Void Queue Example

```go
queue := gocq.NewVoidQueue(2, func(data int) {
    fmt.Printf("Processing: %d\n", data)
    time.Sleep(500 * time.Millisecond)
})
defer queue.WaitAndClose()

// Add jobs
queue.Add(1)
queue.AddAll([]int{2, 3, 4})
```

## 🚀 Performance

The implementation uses efficient data structures:

- Standard Queue: Based on `container/list` with O(1) operations
- Priority Queue: Based on `container/heap` implementation with O(log n) operations

### Benchmark Results

```bash
goos: linux
goarch: amd64
pkg: github.com/fahimfaisaal/gocq
cpu: AMD EPYC 7763 64-Core Processor

BenchmarkQueue_Operations/Add-4                          1000000        1019 ns/op        408 B/op        8 allocs/op
BenchmarkQueue_Operations/AddAll-4                         10000      105456 ns/op      20055 B/op      509 allocs/op
BenchmarkPriorityQueue_Operations/Add-4                  1000000        1028 ns/op        384 B/op        8 allocs/op
BenchmarkPriorityQueue_Operations/AddAll-4                  9600      119258 ns/op      17653 B/op      509 allocs/op
BenchmarkVoidQueue_Operations/Add-4                      1224120       952.3 ns/op        272 B/op        7 allocs/op
BenchmarkVoidQueue_Operations/AddAll-4                     10000      114441 ns/op      16572 B/op       509 allocs/op
BenchmarkVoidPriorityQueue_Operations/Add-4              1256269       953.9 ns/op        272 B/op         7 allocs/op
BenchmarkVoidPriorityQueue_Operations/AddAll-4              9727      117869 ns/op      16573 B/op       509 allocs/op
```

| Queue Type         | Operation | Variant | ns/op  | B/op  | allocs/op |
| ------------------ | --------- | ------- | ------ | ----- | --------- |
| Non-Priority Queue | Add       | Normal  | 1019   | 408   | 8         |
| Non-Priority Queue | AddAll    | Normal  | 105456 | 20055 | 509       |
| Priority Queue     | Add       | Normal  | 1028   | 384   | 8         |
| Priority Queue     | AddAll    | Normal  | 119258 | 17653 | 509       |
| Non-Priority Queue | Add       | Void    | 952.3  | 272   | 7         |
| Non-Priority Queue | AddAll    | Void    | 114441 | 16572 | 509       |
| Priority Queue     | Add       | Void    | 953.9  | 272   | 7         |
| Priority Queue     | AddAll    | Void    | 117869 | 16573 | 509       |

### Run Benchmarks

```bash
go test -bench=. -benchmem
```

## 👤 Author (Fahim Faisaal)

- GitHub: [@fahimfaisaal](https://github.com/fahimfaisaal)
- LinkedIn: [in/fahimfaisaal](https://www.linkedin.com/in/fahimfaisaal/)
- Twitter: [@FahimFaisaal](https://twitter.com/FahimFaisaal)
