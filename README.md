# GoCQ: High-Performance Concurrent Queue for Gophers

Package gocq offers a concurrent queue system using channels and goroutines, supporting both FIFO and priority operations.

[![Go Reference](https://img.shields.io/badge/go-pkg-00ADD8.svg?logo=go)](https://pkg.go.dev/github.com/fahimfaisaal/gocq)
[![Go Report Card](https://goreportcard.com/badge/github.com/fahimfaisaal/gocq)](https://goreportcard.com/report/github.com/fahimfaisaal/gocq)
[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go)](https://golang.org/doc/devel/release.html)
[![CI](https://github.com/fahimfaisaal/gocq/actions/workflows/go.yml/badge.svg)](https://github.com/fahimfaisaal/gocq/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/fahimfaisaal/gocq/branch/main/graph/badge.svg)](https://codecov.io/gh/fahimfaisaal/gocq)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)

GoCQ is a high-performance concurrent queue for Go, optimized for efficient task processing. It supports both FIFO and priority queues, featuring non-blocking job submission, dedicated worker channels, and a pre-allocated worker pool to ensure smooth and controlled concurrency. With optimized memory management, GoCQ minimizes allocations and prevents goroutine leaks, making it a reliable choice for high-throughput applications.

## 🌟 Features

- Generic type support for both data and results
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
  - [Priority Queue](#priority-queue)
- [Examples](#-examples)
- [Performance](#-performance)

## 🔧 Installation

```bash
go get github.com/fahimfaisaal/gocq
```

## 🚀 Quick Start

```go
package main

import (
  "fmt"
  "time"

  "github.com/fahimfaisaal/gocq"
)

func main() {
  // Create a queue with 2 concurrent workers
  queue := gocq.NewQueue(2, func(data int) int {
    time.Sleep(500 * time.Millisecond)
    return data * 2
  })
  defer queue.Close()

  // Add a single job
  result := <-queue.Add(5)
  fmt.Println(result) // Output: 10

  // Add multiple jobs
  results := queue.AddAll(1, 2, 3, 4, 5)
  for result := range results {
    fmt.Println(result) // Output: 2, 4, 6, 8, 10 (unordered)
  }
}
```

## 📚 API Reference

### Standard Queue (FIFO)

#### `NewQueue[T, R any](concurrency uint, worker func(T) R) *ConcurrentQueue[T, R]`

Creates a new concurrent FIFO queue.

- Time Complexity: O(c) where c is concurrency and spawns c goroutines
- Parameters:
  - `concurrency`: Maximum number of concurrent workers
  - `worker`: Function to process each job
- Returns: A new concurrent queue instance

#### Queue Operation Methods

#### `Init() *ConcurrentQueue[T, R]`

Initializes the queue and starts worker goroutines.

- Time Complexity: O(c) where c is concurrency
- Returns: Queue instance for chaining
- Effect: Starts worker goroutines and closes old ones

> Note: Closes old channels to prevent routine leaks

#### `Add(data T) <-chan R`

Adds a single job to the queue.

- Time Complexity: O(1)
- Returns: Channel to receive the result

#### `AddAll(data ...T) <-chan R`

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

#### `WaitUntilFinished()`

Blocks until all pending jobs complete.

- Time Complexity: O(n) where n is number of currently processing and pending jobs
- Effect: Blocks until all pending jobs are processed

#### `Close()`

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

#### `CurrentProcessingCount() uint`

Returns the number of jobs currently being processed.

- Time Complexity: O(1)
- Returns: Number of active jobs

#### `IsPaused() bool`

Checks if the queue is currently paused.

- Time Complexity: O(1)
- Returns: true if paused, false otherwise

### Priority Queue

**The priority queue extends the standard queue with priority support.**

#### `NewPriorityQueue[T, R any](concurrency uint, worker func(T) R) *ConcurrentPriorityQueue[T, R]`

Creates a new concurrent priority queue.

- Time Complexity: O(1)
- Parameters:
  - `concurrency`: Maximum number of concurrent workers
  - `worker`: Function to process each job
- Returns: A new priority queue instance

#### `Add(data T, priority int) <-chan R`

Adds a job with priority (lower number = higher priority).

- Time Complexity: O(log n) where n is queue size
- Parameters:
  - `priority`: Lower value means higher priority
- Returns: Channel to receive the result

#### `AddAll(items []PQItem[T]) <-chan R`

Adds multiple prioritized jobs.

- Time Complexity: O(n log n) where n is number of items
- Returns: Merged channel to receive all results in priority order

## 💡 Examples

### Priority Queue Example

```go
queue := gocq.NewPriorityQueue(1, func(data int) int {
    time.Sleep(500 * time.Millisecond)
    return data * 2
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
    fmt.Println(result) // Output: 6, 4, 2 (processed by priority)
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

## 🚀 Performance

The implementation uses efficient data structures:

- Standard Queue: Based on `container/list` with O(1) operations
- Priority Queue: Based on `container/heap` implementation with O(log n) operations

### Benchmark Results

```bash
goos: linux
goarch: amd64
pkg: github.com/fahimfaisaal/gocq
cpu: 13th Gen Intel(R) Core(TM) i7-13700
BenchmarkPriorityQueue_Operations/Add-24                 1378249              1278 ns/op
BenchmarkPriorityQueue_Operations/AddAll-24               795332              1712 ns/op
BenchmarkQueue_Operations/Add-24                         1000000              1300 ns/op
BenchmarkQueue_Operations/AddAll-24                      1000000              1822 ns/op
```

## 👤 Author (Fahim Faisaal)

- GitHub: [@fahimfaisaal](https://github.com/fahimfaisaal)
- LinkedIn: [in/fahimfaisaal](https://www.linkedin.com/in/fahimfaisaal/)
- Twitter: [@FahimFaisaal](https://twitter.com/FahimFaisaal)
