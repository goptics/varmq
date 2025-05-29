# Changelog

## [v1.2.0] (2025-05-29)

### ✨ What's New in v1.2.0

- **Dynamic Worker Pool Tuning**:
  - `worker.TunePool(concurrency int)`: Allows dynamic adjustment of the worker pool size at runtime.
- **Idle Worker Management**:
  - Configuration option `WithMinIdleWorkerRatio` for fine-grained control over the minimum ratio of idle workers.
  - Configuration option `WithIdleWorkerExpiryDuration` for setting the expiry duration of idle workers, optimizing resource usage.
- Added `worker.NumIdleWorkers()` to get the number of idle workers.
- Added `worker.NumConcurrency()` to get the current number of concurrency.

### 📊 Performance Improvements

- **`Add` Operation Benchmark:**
  - _Previous Version_: 384 Bytes/operation, 8 allocations/operation
  - _Current Version (v1.2.0)_: 122 Bytes/operation, 3 allocations/operation
  - _Impact_: Approximately 68% reduction in memory usage and 62.5% reduction in allocations per operation.

### 📈 Improvements

- **Pool Management**: Enhanced control over worker pool lifecycle and resource allocation with new tuning and idle management features.
- **Wait Management**: Centralized and clarified wait/stop logic on the `IWorker` interface and replaced wait group implementation by channels (`WaitUntilFinished`, `WaitAndStop`).
- **Job Management**:
  - The `Job[T]` interface, now passed to all worker functions, offers richer interaction with job data and metadata (e.g., `job.ID()`, `job.Data()`).
  - Single job can be closeable.
- **Group Job Management**:
  - Improved handling of batch operations with `EnqueuedGroupJob[T,R]` interface, providing methods like `NumPending()`, `Results()`, `Errors()` and `Wait()`

### ➖ Removed Features

- **Built-in Cache**:
  - Removed `WithCache` and `WithAutoCleanupCache` configuration options. Cache management is now externalized.
- **Persistent Queue `AddAll`**:
  - The `AddAll` batch submission feature has been removed from persistent queue implementations.
- **Internal Job Management for Persistent Queues (via Cache)**:
  - With the removal of the internal cache, job state management for persistent queues relies more directly on the specific persistent adapter implementation.

### 💥 Breaking Changes

This version introduces significant API changes for improved clarity, consistency, and functionality.

| Old API Element                               | New API Element                                            | Description                                                                                                                                              |
| --------------------------------------------- | ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `w.CurrentProcessingCount()`                  | `w.NumProcessing()`                                        | Renamed worker method for getting the count of currently processing jobs.                                                                                |
| `q.PendingCount()`                            | `q.NumPending()`                                           | Renamed queue method for getting the count of pending jobs.                                                                                              |
| `PQItem[T]` struct                            | Merged into `Item[T]` (or direct priority in `Add`)        | Priority queue item handling simplified. `Item[T]` is now used (primarily for `AddAll` operations), removing the need for a separate `PQItem[T]` struct. |
| `NewWorker[T, R](wf func(T) (R, error), ...)` | `NewResultWorker[T, R](wf func(j Job[T]) (R, error), ...)` | Worker for tasks returning a result and error. Worker function now receives `Job[T]`.                                                                    |
| `NewVoidWorker[T](wf func(T), ...)`           | `NewWorker[T](wf func(j Job[T]), ...)`                     | Worker for fire-and-forget tasks. Renamed and worker function now receives `Job[T]`. This is also the primary worker for persistent/distributed queues.  |
| `NewErrWorker[T](wf func(T) error, ...)`      | `NewErrWorker[T](wf func(j Job[T]) error, ...)`            | Worker for tasks returning only an error. Worker function now receives `Job[T]`.                                                                         |
| `queue.WaitUntilFinished()`                   | `worker.WaitUntilFinished()`                               | Responsibility for waiting for all jobs to complete moved from queue to worker.                                                                          |
| `queue.WaitAndClose()`                        | `worker.WaitAndStop()`                                     | Responsibility for waiting and stopping processing moved from queue to worker.                                                                           |

---

## [v1.1.0]

- 📦 Bump Release

---

## [v1.0.0] (2025-05-01)

### 🔄 Project Evolution: GoCQ → VarMQ

- **Project Rebranding**: Renamed from GoCQ to VarMQ
- **Organization Move**: Moved from personal repository `github.com/fahimfaisaal/gocq` to organization `github.com/goptics/varmq`
- **Architectural Redesign**: Shifted from a simple concurrent queue to a full message queue system with adaptable backends

### ✨ Expanded Capabilities

- **Advanced Queue Variants**:
  - Added Persistent Queue support for data durability across restarts
  - Added Distributed Queue support for scaling across multiple systems
  - Maintained Priority Queue functionality with improvements
- **Worker Abstraction Layer**:
  - Introduced explicit worker types and interfaces (`NewWorker`, `NewErrWorker`, `NewVoidWorker`)
  - Added support for different worker patterns (result-returning, error-only, void)
  - Simplified worker creation with dedicated factory methods

### 🔧 API Improvements

- **Worker-Centric Design**:

  - New approach: Create worker first, then bind to desired queue type
  - Old: `queue := NewQueue(concurrency, workerFunc)`
  - New: `worker := NewWorker(workerFunc)` followed by `queue := worker.BindQueue()`

- **Enhanced Job Control**:

  - Replaced simple channel returns with comprehensive Job interface
  - Added job status tracking, explicit result methods, and error handling
  - New job management methods like `Status()`, `IsClosed()`, `Result()`, and `Drain()`

- **Batch Processing**:
  - Improved batch job handling with dedicated group job interfaces
  - Enhanced result collection mechanisms for batch operations

### 🚀 Production Readiness

- **Extensibility**:
  - Added adapter interfaces for creating custom queue backends
  - Support for external storage systems like Redis (via optional adapters)
- **Documentation**:
  - Comprehensive API reference with detailed examples
  - Visual architecture diagrams and sequence flows
  - Clearer usage patterns and best practices

### 🛠️ Technical Improvements

- **Performance Optimizations**:

  - Reduced goroutine overhead in batch operations
  - Improved memory efficiency and resource management
  - Enhanced concurrency control mechanisms

- **Type Safety**:
  - Strengthened generic constraints
  - Better error and panic propagation and handling

## Gocq - end release

## [v2.0.0] (2025-03-14)

### 🔄 Breaking Changes

- Complete API redesign for better type safety and error handling
- Replaced channel-based result handling with Job interface
- Introduced new Job types for better control and status tracking
- Separated void (non-returning) queues into dedicated implementations

### ✨ New Features

- Added `EnqueuedJob` interface for handling individual job results
- Added `EnqueuedGroupJob` interface for handling batch job results
- Introduced void queue variants for operations without return values
- Added job status tracking (`Status()` method)
- Added `IsClosed()` method to check job state
- Added `Drain()` method for discarding results
- Enhanced error handling with dedicated error channels
- Added `Result()` and `WaitForError()` method for synchronous result retrieval
- Added `Errors()` method for retrieving errors from void group jobs
- Added `Results()` method for retrieving results from group jobs
- Added `Close()` method for closing jobs and associated channels

### 🔧 API Changes

#### Queue Creation

- Old: `NewQueue[T, R](concurrency uint, worker func(T) R)`
- New: `NewQueue[T, R](concurrency uint32, worker WorkerFunc[T, R])`

#### Job Submission

- Old: `Add(data T) <-chan R`
- New: `Add(data T) EnqueuedJob[R]`

#### Batch Operations

- Old: `AddAll(data ...T) <-chan R`
- New: `AddAll(data []T) EnqueuedGroupJob[R]`

#### Priority Queue

- Old: `Add(data T, priority int) <-chan R`
- New: `Add(data T, priority int) EnqueuedJob[R]`
- Old: `AddAll(items []PQItem[T]) <-chan R`
- New: `AddAll(items []PQItem[T]) EnqueuedGroupJob[R]`

### 🚀 Performance Improvements

- Optimized memory usage with better channel management
- Reduced goroutine overhead
- Improved priority queue operations
- Reduced spawning of n number of goroutines to constant inside `AddAll` method

### 🛠️ Technical Improvements

- Enhanced type safety with generic constraints
- Better resource cleanup mechanisms
- Improved concurrency control
- Added comprehensive error handling
- Thread-safe operations guaranteed

### 📚 Documentation

- Updated API reference with new interfaces and methods
- Added more code examples
- Improved documentation clarity
- Added performance benchmarks

### 🔧 Other Changes

- Minimum Go version requirement updated to 1.24+
- Internal refactoring for better maintainability
- Enhanced test coverage
- Added benchmarks for normal and void queue operations

## [v1.0.0] (Gocq Initial Release) - 2025-03-07

### 🎉 Initial Release

VarMQ (Go Concurrent Queue) is a high-performance concurrent queue system for Go, featuring both FIFO and priority queue implementations.

### ✨ Features

- Generic type support for both data and results
- Configurable worker pool with controlled concurrency
- Two queue implementations:
  - Standard FIFO Queue with O(1) operations
  - Priority Queue with O(log n) operations
- Non-blocking job submission
- Thread-safe operations
- Pause/Resume functionality
- Clean shutdown mechanisms
- Comprehensive test coverage

### 🔧 Technical Details

- Minimum Go version: 1.24+
- Standard Queue: Based on `container/list`
- Priority Queue: Based on `container/heap`
- Thread safety using sync primitives
- Memory-efficient channel management

### 🚀 Performance

Benchmark results on Intel i7-13700:

```bash
goos: linux
goarch: amd64
pkg: github.com/fahimfaisaal/gocq/v2
cpu: 13th Gen Intel(R) Core(TM) i7-13700
BenchmarkPriorityQueue_Operations/Add-24                 1378249              1278 ns/op
BenchmarkPriorityQueue_Operations/AddAll-24               795332              1712 ns/op
BenchmarkQueue_Operations/Add-24                         1000000              1300 ns/op
BenchmarkQueue_Operations/AddAll-24                      1000000              1822 ns/op
```

### 📝 License

MIT License - See LICENSE file for details
