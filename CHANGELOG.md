# Changelog

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

## [v1.0.0] (2025-03-7)

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
