# Changelog

## [Unreleased]

### ✨ New Features

- **In-memory dedupe by job id**: queue and priority queue (plain, error, result) store one waiting job per id given with `WithJobId` or `Item.ID`. `Add` of an id already in queue returns `nil, false`; `AddAll` skips that item. Id free again once worker takes job or `Purge` drops it. Generated ids not checked (generator never repeats one for same work); persistent and distributed queues still check them. Jobs without id: no cost, except a type check on dequeue once the queue has stored a job with an id. A drained id set over 1024 ids is dropped, so a burst does not keep its peak memory. In-memory storage implements `IDedupQueue` / `IDedupPriorityQueue`, returning `false` for repeat (persistent storage returns `true`).
  - **Behavioral change**: in-memory `Add` reusing an id while a job with it is queued now returns `false`.
  - **Behavioral change**: `AddAll` items without id now have job id `""`, not `"g:"`.

### 📚 Documentation

- README benchmarks moved to [goptics/benchmarks](https://github.com/goptics/benchmarks), with Redis and SQLite queue comparisons (#128).

## [v1.9.0] (2026-10-18)

### ✨ New Features

- **Topic fan-out** (#104): `NewTopic[T]()` + `IWorkerBinder.WithTopic(topic)`. `Publish` stores payload once in shared log; each subscription reads at own cursor, its worker gets own job. Fanout-exchange semantics: no replay, late subscribers miss earlier payloads. Void workers (`NewWorker`) only.
  - `Publish` returns `false` when no subscription, topic closed, or full.
  - `WithTopic` returns `Subscription` (`Len`, `IsFull`, `Purge`, `Close`, `Worker`, `Unsubscribe`), no `Add`. `Close` = `Unsubscribe`.
  - **Breaking for implementers/mocks**: `IWorkerBinder[T]` gained `WithTopic`.
- **Topic priority**: `WithTopicPriority(n)` orders topic subscriptions among worker's sources, like `WithQueuePriority`. Default `math.MaxInt`.
- **Topic capacity**: `WithTopicCapacity(n)` on `NewTopic`. `Publish` refused once slowest subscription has `n` unread, so stalled subscriber can't grow memory forever. `Topic.IsFull()`, `Subscription.IsFull()` (is this one holding it full). Default unlimited.
- **One subscription per worker per topic**: subscribing twice panics (would run every payload twice). Resubscribe after `Unsubscribe`/`Close` OK.
- **REST instance header** (#101): every REST response sets `X-Varmq-Instance` (`InstanceHeader`) to `InstanceID`, default hostname (pod name on k8s). Tells which instance answered behind load balancer. `Handler` sets it on `ServeMux` 404/405 too.
  - **Behavioral change**: worker-endpoint errors now JSON `{"error": "<message>"}`, not plain text. Status codes same. `ServeMux` own 404/405 stay plain text.

### ⚡ Performance

- **Batched dispatch** (#107): event loop hands worker up to 32 jobs per wake-up from in-memory FIFO queues and topics. Fixes dispatcher ceiling (~3M jobs/s flat across CPU counts). Priority, acknowledging (persistent/distributed) and multi-source workers stay at 1 job per hand-off.
- **Less lock contention** (#106): processing slot released atomically, `w.mx` only when count hits 0. Idle worker pool now lock-free Treiber stack (removals serialised, no ABA). ~2x throughput on varmq-benchmarks. `internal/linkedlist` removed.
- **Lock-free appends** (#111): `Add` on in-memory FIFO queue and `Publish` on unbounded topic claim slot with one atomic add. Bounded topics still lock so bound exact. 100 producers, 1M jobs: 222 → 145 ms (4 CPU), 496 → 123 ms (24 CPU).
- **Lock-free topic reads**: subscriptions read only published slots under own lock. Reads never block publishes. 100 subscribers: publish 9.6 → 0.27 µs.
- **Queue and topic on linked chunks**: fixed 1024-slot chunks, burst never copies backlog, read chunk GC'd once slowest cursor passes. FIFO queue reuses topic log with consuming cursor, ~25% faster enqueue+dequeue. `internal/linkedbuffer` removed.
- **Smaller jobs** (#114): in-memory job 80 → 56 B (ack data behind pointer only acknowledging storage allocates). Error/result jobs store outcome inline, no channel: 1 alloc per `Add` (278 → 110 B error, 326 → 102 B result). Priority heap stores items by value: 112 → 64 B. `AddAll` of 1000: 3005 → 1004 allocs, ~30% faster; ID generator runs only for items without ID.
- **Job sources**: event loop pulls from internal `source` interface chosen at bind time, no per-job storage check. One source: direct pull; strategy only with 2+ sources.
  - Fixed side effect: custom `IAcknowledgeable` queue storing job objects (not JSON) now acks on completion. Failed ack → `ErrAcknowledgeJob`, job stays `Finished`, `Wait` on it doesn't return (same as persistent queues).

### 🐛 Bug Fixes

- **Slow jobs buffered behind each other** (#116): batch size now follows dispatcher load, not count alone. Workers get just enough jobs to come back ~every 32 µs; unmeasured worker takes 1. 10 workers on 3 s jobs → 1 job each, queue order kept. Many workers on 10 ms jobs still batch 32.
- **Stop and Pause ran buffered jobs** (#116): worker finishes running job, parks rest of batch (still `Queued`, counted by `NumPending`). `Restart`/`Resume` hand parked jobs out first, in dispatch order. `Purge` closes parked jobs. Concurrency 1 keeps full batches.
- **Double Close panic** (#115): two racing `Close` calls could both release → `sync: negative WaitGroup counter`. Move to `Closed` now one CAS: one wins, other gets `ErrJobAlreadyClosed`. Ack happens once, before claim. `Add` no longer overwrites `Closed`/running state. Group jobs close shared `Results()`/`Errs()` channel once.
- **Purge skipped batched jobs** (#112): `Purge` on in-memory queue or subscription now also cancels jobs already in a worker's batch (per-source counter checked before each job).
- **Chunk link race** (#113): one `Append` links next chunk; others yield briefly before allocating own spare, instead of all allocating at once.
- **Priority queue kept popped jobs alive**: popped heap slot now cleared; `Purge` drops backing array.
- **Wait hung on shared source**: `Wait`/`WaitUntilIdle` re-check every 10 ms while idle, so a source drained by another worker or process no longer hangs them.
- **Enqueue racing Close**: in-memory `Enqueue` checks closed flag under queue lock; no item lands after `Close` + `Purge`.

## [v1.8.0] (2026-07-04)

### ✨ New Features

- **Callback-Based Error Handler** (#81): Added `WithErrHandler(cb func(Worker, error))` to set a per-worker error handler callback, and `DefaultErrHandler(cb func(Worker, error))` to set the default error handler for all newly created workers.
  - The error handler is invoked for all internal errors encountered during job processing (dequeue failures, parse failures, cast failures, etc.).
  - Replaces the need to listen on the `Errs()` channel for simple error handling scenarios.
  - Nil-safe: passing `nil` as the callback silently uses a no-op handler.

### 🔧 Refactoring

- **Queue Picker Error Flow** (#80): Changed the `queueManager.next()` to return a `bool` instead of an `error`, simplifying the event loop logic. The `ErrGetNextQueue` sentinel error and `ErrNoItemsRegistered`/`ErrAllItemsEmpty` error variables from `internal/helpers/manager.go` were removed in favor of boolean returns, reducing allocations and improving code clarity.
  - `Manager.GetMaxLenItem()`, `GetMinLenItem()`, `GetPriorityItem()`, and `GetRoundRobinItem()` now return `(T, bool)` instead of `(T, error)`.

## [v1.7.0] (2026-05-16)

### ✨ New Features

- **REST API** (#80): Built-in HTTP endpoints to inspect and manage workers remotely.
  - `GET /health` — health check
  - `GET /workers` — list all workers (summary)
  - `GET /workers/{name}` — detailed worker info + metrics
  - `PATCH /workers/{name}/actions/{action}` — pause, resume, stop, restart, start
  - `PATCH /workers/{name}/config/concurrency/{n}` — adjust concurrency
  - Register via `varmq.Handler("/varmq")` or `varmq.NewServerMux()`
  - **Security note**: endpoints are unauthenticated by design — protect with middleware, network isolation, or a reverse proxy before production use.

- **Lifecycle State Machine** (#72): Introduced a formal state machine with 8 distinct worker states: `Initiated`, `Idle`, `Running`, `Pausing`, `Paused`, `Stopping`, `Stopped`, and `Unknown`.
  - `Status()` returns the human-readable current state.
  - `WaitUntilPaused()` — blocks until the worker reaches the `Paused` state.
  - `WaitUntilStopped()` — blocks until the worker reaches the `Stopped` state.
  - `Wait()` — universal await: returns when all in-flight jobs complete regardless of final state.
  - **Behavioral change**: `WaitUntilIdle()` now specifically waits for the `Idle` state (not just "work done"). If the worker is paused or stopped with no work, it blocks indefinitely — use `Wait()` instead when you only care that work is finished.

- **Stopped Retention for Worker Registry**: Workers in the `Stopped` state are retained in the global `WorkerRegistry` (not auto-removed), allowing the REST API to report on stopped workers.

- **Start Action in Mux Handler**: Added `start` as a valid action in `PATCH /workers/{name}/actions/start`.

### 🐛 Bug Fixes

- **Condition Variable Race Conditions** (#78): Fixed race conditions in the waiter system caused by improper condition variable synchronization.
- **Named Worker Registration**: Fixed an issue where unnamed workers were being registered in the global `WorkerRegistry`. Only workers with an explicit name are now registered.
- **Flaky Waiter Tests**: Stabilized flaky tests in the worker lifecycle test suite and removed redundant test cases.

### 🔧 Refactoring

- **Worker Module Decomposition**: Split the monolithic `worker.go` (~1,200 lines) into focused modules:
  - `worker_state.go` — status constants and state machine logic
  - `worker_lifecycle.go` — pause, stop, restart, start implementations
  - `worker_eventloop.go` — event loop and job dispatch
  - `worker_pool.go` — pool management and concurrency tuning
  - `worker_wait.go` — wait semantics and condition variable handling
- **Context Management** (#74): Moved worker context lifecycle management into the event loop for cleaner shutdown semantics.
- **Processing Cleanup Deduplication** (#77): Extracted `releaseProcessingSlot` to reduce redundant cleanup logic across waiters.
- **Universal `Wait`** (#73): Refactored `Wait()` to work correctly across all worker states (Running, Idle, Pausing, Paused, Stopping, Stopped).

### 📚 Documentation

- **README**: Added REST API section with endpoint table and security warning, updated benchmark data, added GoDoc link.
- **Removed `docs/API_REFERENCE.md`**: Prefer GoDoc for complete package documentation.
- **Status() GoDoc**: Updated to list all 8 possible return values matching the implementation.

### 📈 Impact Summary

This release introduces a REST API for remote worker management, a formal state machine with 8 lifecycle states, and a major internal refactoring that decomposes the worker module into focused, maintainable files. The `WaitUntilIdle()` behavior change ensures state-specific waiting semantics — use `Wait()` when you only need to know that work is done.


## [v1.6.1] (2026-03-13)

### 🐛 Bug Fixes

- **Worker Pause State**: Fixed an issue where `releaseWaiters` overwrites the worker status to `idle` when the last in-flight job completes, even if the worker had been explicitly paused. The fix uses `status.CompareAndSwap(running, idle)` so the transition only occurs from the `running` state, effectively preserving the paused status.


## [v1.6.0] (2026-03-08)

### ✨ New Features

- **Idle Worker Status** (#69): Introduced a distinct `idle` status for workers to differentiate between workers actively processing jobs and workers that are active but waiting for new jobs.
  - Added `IsIdle()` method to check if the worker is idle (active but not processing).
  - Added `IsActive()` method to check if the worker is active (either running or idle).
  - Added `WaitUntilIdle()` method that waits until all pending jobs in the queue are processed and the worker transitions to idle.
  - Worker event loop now dynamically transitions between `running` and `idle` based on workload and processing status.

- **Queue Capacity** (#68): Added `WithQueueCapacity(cap int)` queue configuration option to set a maximum queue size.
  - `IsFull()` method returns `true` when the queue has reached its configured capacity.
  - `Add()` returns `false` as the second return value when the queue is full.

### 🐛 Bug Fixes

- **Metrics Completion Tracking**: Moved `w.metrics.incCompleted()` call earlier in job completion logic to ensure accurate metrics reporting.

### ⚠️ Deprecations

- **`queue.NumPending()`**: Deprecated in favor of `queue.Len()` for a more idiomatic Go API. `NumPending()` will be removed in an upcoming release.
- **`worker.WaitUntilFinished()`**: Deprecated in favor of `worker.WaitUntilIdle()` which better describes the behavior of waiting until all pending jobs are processed and the worker is idle.

### 📚 Documentation

- Added `configs` parameter documentation to queue methods godoc.
- Removed extra spaces from code examples.

## [v1.5.0] (2026-02-24)

### 🔄 Behavioral Changes

- **Default Queue Strategy**: The default multi-queue binding selection strategy has changed from `RoundRobin` to `Priority`. Queues binded to a worker will now process based on Priority rather than alternating evenly by default.

### ✨ New Features

- **Queue Priority Configuration** (#66): 
  - Introduced `QueueConfig` and `QueueConfigFunc` to allow configuring queue attributes when binding them to workers. 
  - Added `WithQueuePriority` to let users assign processing priorities to individual standard, priority, persistent, and distributed queues.
  - Updated `Bind*` and `With*` methods across worker interfaces to accept variadic `QueueConfigFunc` arguments.
- **Default Configuration Modifiers** (#65): Added predefined standalone functions (`DefaultConcurrency`, `DefaultStrategy`, `DefaultMinIdleWorkerRatio`, `DefaultJobIdGenerator`, `DefaultIdleWorkerExpiryDuration`, `DefaultCtx`) to explicitly and globally modify default configurations for all newly created workers.

### 🐛 Bug Fixes

- **Worker Event Loop Fix** (#64): Removed redundant recursive `processNextJob` calls inside workers. The event loop already retries processing properly, so removing the recursion prevents potential and silent error swallowing.

### 🔧 Refactoring and Code Quality

- **Panic-Safety in Worker Shutdown**: Removed redundant status checks in workers. The reliance on nil channel behaviors, `mutex` locks, and `select` default cases provides streamlined, safe shutdown event handling without explicit and extra condition checks.


## [v1.4.0] (2026-02-05)

### ✨ New Features

- **Public Sentinel Errors** (#61):

  - Exported all internal errors for better error handling by end users
  - Users can now use `errors.Is()` to detect specific error types

  **Worker Errors:**
  - `ErrRunningWorker` - returned when attempting to start an already running worker
  - `ErrNotRunningWorker` - returned when attempting operations on a non-running worker
  - `ErrSameConcurrency` - returned when tuning pool to the same concurrency value
  - `ErrFailedToDequeue` - returned when dequeue operation fails
  - `ErrFailedToCastJob` - returned when job type casting fails
  - `ErrGetNextQueue` - returned when getting next queue fails (wraps underlying error)
  - `ErrParseJob` - returned when job parsing fails (wraps underlying error)

  **Job Errors:**
  - `ErrJobProcessing` - returned when attempting to close a processing job
  - `ErrJobAlreadyClosed` - returned when attempting to close an already closed job
  - `ErrAcknowledgeJob` - returned when queue acknowledgement fails (wraps context)

  **Example Usage:**
  ```go
  err := worker.TunePool(5)
  if errors.Is(err, varmq.ErrNotRunningWorker) {
      // Handle worker not running
  }
  ```

- **Worker Context Configuration** (#60):

  - Added `WithContext(ctx context.Context)` configuration option for workers
  - Workers can now be stopped gracefully when the parent context is cancelled
  - Enables integration with application lifecycle management and graceful shutdown patterns

- **Worker Error Listener** (#58):

  - Added `Errs() <-chan error` method to the `Worker` interface
  - Provides a read-only channel to listen to all internal errors during job processing
  - Initialized static errors at compile time to reduce over allocations

- **Worker Metrics** (#57):

  - Added `Metrics() Metrics` method to track worker statistics
  - Includes `Completed()` and `Submitted()` counters for job tracking
  - Added new `NumPending() int` method to provide the number of pending tasks
  - Added Prometheus example to demonstrate how to integrate VarMQ with monitoring systems

### 🐛 Bug Fixes

- **Worker Lifecycle Management** (#59):

  - Resolved worker restart issue and related methods
  - Refined `Stop()`, `Pause()`, and `Restart()` behavior for better reliability
  - Improved overall test coverage for lifecycle methods

- **Metrics & Acknowledgment Fixes**:
  - Fixed submitted metrics increment for distributed queues
  - Consider job acknowledgment error as nil to prevent false error propagation

- **Memory Optimization** (#54):
  - Removed extra 1-byte memory padding from configs struct


## [v1.3.1] (2025-06-30)

### 🔧 Refactoring and Code Quality

- **Pool Structure Enhancement**:
  - Renamed constructor function of Pool for better naming consistency (#48)
  - Updated worker implementation to use new pool structure (#47)
  - Replaced mockQueue with NewQueue to reduce redundant code and improve maintainability (#46)

### 🧪 Testing Improvements

- **Enhanced Test Coverage**:
  - Added comprehensive distributed and persistent queue tests with edge cases (#44)
  - Updated assertions in worker binder tests for better validation of pending jobs

### 📚 Documentation and Assets

- **Performance Documentation**:

  - Enhanced README with detailed benchmarking information and comparison with Pond v2 (#45)
  - Added interactive charts and benchmark repository links for performance analysis

- **Asset Optimization**:
  - Compressed architecture diagram image size to 3MB for better loading performance
  - Reduced arch diagram image size for improved repository efficiency

### 🛠️ Project Configuration

- **Development Workflow**:
  - Updated project configuration and added GitHub issue templates
  - Improved contributor experience with better issue reporting structure

### 📈 Impact Summary

This patch release focuses on code quality improvements, enhanced testing coverage, and better documentation. The refactoring efforts improve maintainability while the expanded test suite ensures reliability of distributed and persistent queue functionality.

## [v1.3.0] (2025-06-21)

### ✨ What's New in v1.3.0

- **Multi-Queue Binding**:

  - Single worker can now bind to multiple queues with configurable selection strategies.
  - `WithStrategy(strategy Strategy)` configuration option for queue selection behavior.
  - Built-in strategies: RoundRobin, MaxLen and MinLen for flexible job processing patterns.

- **Function Helper Methods**:

  - `Func()`: Direct function submission for synchronous execution without return values.
  - `ErrFunc()`: Error-returning function submission with propagated error handling.
  - `ResultFunc[R]()`: Generic result-returning function submission with type-safe results.

- **Enhanced Queue Implementation**:
  - Implemented LinkedBufferQueue with dynamic, chunk-based buffer structure (up to 70% low mem uses on high load).
  - Automatic chunk management with optimized growth strategy (inspired by pond v2).

### 📊 Performance Improvements

- **Memory Usage Optimization**:

  - _LinkedBufferQueue vs SliceQueue_: 62% fewer bytes allocated (82KB/op vs 217KB/op)
  - _Operation Speed_: ~46% faster queue operations (238ns/op vs 442ns/op)
  - Zero memory copy when reading across chunk boundaries

- **Reduced Allocations**:
  - Sync pool implementation for worker nodes
  - Optimized memory reuse pattern for job processing
  - Enhanced job management through controlled termination

### 📈 Improvements

- **Queue Management**:

  - Improved closing behavior allowing existing items to be processed
  - Better synchronization between workers and queues

- **Worker Synchronization**:
  - Enhanced `WaitUntilFinished()` implementation with proper condition checks
  - Eliminated redundant notifications to waiting processes
  - Improved worker state management during queue transitions

### 📚 Documentation Updates

- Added comprehensive benchmark comparisons and charts
- Improved API reference clarity and organization
- Enhanced README with clearer descriptions and examples
- Added documentation for multi-queue binding and function helpers

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
