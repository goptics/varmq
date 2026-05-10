# worker.go File-Split Refactoring Design

**Date:** 2026-05-10
**Status:** Draft

## Goal

Split the 962-line `worker.go` monolith into focused files by responsibility, improving maintainability and testability. The `Worker` interface and all public APIs remain unchanged — this is an internal reorganization only.

## Motivation

- `worker.go` at 962 lines mixes state machine, event loop, pool management, wait semantics, and lifecycle orchestration in one file
- Hard to navigate: finding a specific method requires scrolling through unrelated concerns
- Hard to test: all tests go into one `worker_test.go` file, mixing concerns
- The package already follows a file-per-concern pattern (`queue.go`, `job.go`, `config.go`) — worker.go is the outlier

## Approach

**File-split only (Approach A).** Methods stay on the same `worker[T, JobType]` struct. No new types, no new interfaces, no risk of regression.

## Target File Structure

| File | Contents | ~Lines |
|------|----------|--------|
| `worker.go` | Constants, errors, struct definition, `Worker` interface, constructors (`newWorker`, `newErrWorker`, `newResultWorker`), simple getters (`Name`, `Metrics`, `Context`, `Is*`, `Num*`, `Errs`) | ~180 |
| `worker_state.go` | State machine: `Pause()`, `Resume()`, `getStatusError()`, `Status()` | ~80 |
| `worker_wait.go` | `releaseWaiters()`, `Wait()`, `WaitUntilIdle()`, `WaitUntilPaused()`, `WaitUntilStopped()`, composite methods (`PauseAndWait`, `WaitAndPause`, `StopAndWait`, `WaitAndStop`) | ~140 |
| `worker_lifecycle.go` | `Start()`, `Stop()`, `Restart()` | ~130 |
| `worker_eventloop.go` | `goEventLoop()`, `processNextJob()`, `notifyToPullNextJobs()`, `releaseProcessingSlot()`, `sendToNextChannel()` | ~160 |
| `worker_pool.go` | `initPoolNode()`, `freePoolNode()`, `removeAllWorkers()`, `TunePool()`, `goRemoveIdleWorkers()`, `numMinIdleWorkers()` | ~180 |

## Order of Extraction

Least-coupled files moved first, building toward the most-dependent:

1. **`worker_state.go`** — only depends on atomic status + mutex, no dependency on pool or event loop
2. **`worker_wait.go`** — depends on atomic status + curProcessing + queues.Len + sync.Cond
3. **`worker_pool.go`** — depends on pool package, concurrency atomic
4. **`worker_eventloop.go`** — depends on pool management + queues + atomic state
5. **`worker_lifecycle.go`** — orchestrates event loop + pool + state, depends on all of the above

## Test Strategy

- Existing tests continue passing without modification (same package, same struct)
- Each new file gets a matching `_test.go`:
  - `worker_state_test.go`
  - `worker_wait_test.go`
  - `worker_pool_test.go`
  - `worker_eventloop_test.go`
  - `worker_lifecycle_test.go`
- Verification: `make test` after each file move

## Risks

**None.** This is pure code movement within the same Go package. The compiler guarantees correctness — if a method references a private field from another file in the same package, it's valid. The only operational risk is merge conflicts on the active `feat/mux` branch.

## What This Does NOT Do

- Does not introduce new types or interfaces
- Does not change the Worker interface or any public API
- Does not refactor internal logic — only reorganizes
- Does not touch `worker_binder.go`, `queue.go`, or any other files
