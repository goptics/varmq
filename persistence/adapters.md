# Adapters

The queue interfaces that a storage adapter implements, and what the worker calls on each one.

A storage adapter is a type that implements some of VarMQ's queue interfaces. redisq and sqliteq are adapters. A type of your own can also be one. VarMQ decides what a queue can do by the interfaces its value implements. It checks them with a type assertion when you bind the queue or add a job. If an interface is not implemented, its behaviour is off. This is not an error. This page lists them all, from [`IBaseQueue`](https://pkg.go.dev/github.com/goptics/varmq#IBaseQueue) to the small job interfaces. They are all in the `varmq` package.

For using redisq or sqliteq, read [Redis](https://varmq.goptics.org/persistence/redis/) and [SQLite](https://varmq.goptics.org/persistence/sqlite/) instead. [The overview](https://varmq.goptics.org/persistence/overview/) covers what persistent and distributed queues do.

The map below shows how the interfaces fit together. Press a part to say that your type has it, and the boxes that combine parts switch on. Press a box to read what it adds, what the worker calls on it and when. The start-from buttons load redisq and sqliteq as the table at the end of the page describes them.

## What you pass and what you get

| You call | Storage must implement | You get |
|---|---|---|
| [`WithQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithQueue) | [`IQueue`](https://pkg.go.dev/github.com/goptics/varmq#IQueue) | [`Queue[T]`](https://pkg.go.dev/github.com/goptics/varmq#Queue) |
| [`WithPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithPriorityQueue) | [`IPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPriorityQueue) | [`PriorityQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#PriorityQueue) |
| [`WithPersistentQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithPersistentQueue) | [`IPersistentQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPersistentQueue) | [`PersistentQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#PersistentQueue) |
| [`WithPersistentPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithPersistentPriorityQueue) | [`IPersistentPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPersistentPriorityQueue) | [`PersistentPriorityQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#PersistentPriorityQueue) |
| [`WithDistributedQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithDistributedQueue) | [`IDistributedQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDistributedQueue) | [`DistributedQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#DistributedQueue) |
| [`WithDistributedPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithDistributedPriorityQueue) | [`IDistributedPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDistributedPriorityQueue) | [`DistributedPriorityQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#DistributedPriorityQueue) |
| [`NewDistributedQueue`](https://pkg.go.dev/github.com/goptics/varmq#NewDistributedQueue), no worker | `IDistributedQueue` | `DistributedQueue[T]` |
| [`NewDistributedPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#NewDistributedPriorityQueue), no worker | `IDistributedPriorityQueue` | `DistributedPriorityQueue[T]` |

Queues store values of type `any`. An in-memory queue, and one you bind with `WithQueue` or `WithPriorityQueue`, stores the job values VarMQ creates. [`Add`](https://pkg.go.dev/github.com/goptics/varmq#PersistentQueue.Add) on a persistent or distributed queue encodes the job as JSON and passes the `[]byte` to [`Enqueue`](https://pkg.go.dev/github.com/goptics/varmq#IQueue.Enqueue). What the storage gives back has to be that `[]byte`, or a job value. Anything else is reported as [`ErrFailedToCastJob`](https://pkg.go.dev/github.com/goptics/varmq#ErrFailedToCastJob), and bytes that do not decode are [`ErrParseJob`](https://pkg.go.dev/github.com/goptics/varmq#ErrParseJob).

What the worker assumes of any queue:

- **It is safe to call from several goroutines.** Callers add jobs from their own goroutines. The worker's event loop reads. Every job's goroutine acknowledges when it ends.
- **A read that finds nothing returns `false` for `ok`.** The worker checks [`Len`](https://pkg.go.dev/github.com/goptics/varmq#IBaseQueue.Len) before it reads. If a read then finds nothing, for example because another consumer took the job meanwhile, the worker reports [`ErrFailedToDequeue`](https://pkg.go.dev/github.com/goptics/varmq#ErrFailedToDequeue) on [`Errs()`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Errs) and to the handler.
- **Optional interfaces are found on the value you pass.** A wrapper can hold the adapter behind an interface such as `IQueue`. Such a wrapper hides [`IDedupQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDedupQueue), [`IBatchAcknowledgeable`](https://pkg.go.dev/github.com/goptics/varmq#IBatchAcknowledgeable) and [`CapacitySetter`](https://pkg.go.dev/github.com/goptics/varmq#CapacitySetter). The behaviour they add is then off.

## The queue interfaces

### `IBaseQueue`

The root of every queue. Every queue you give to VarMQ has these five methods:

| Method | The worker calls it |
|---|---|
| [`Len() int`](https://pkg.go.dev/github.com/goptics/varmq#IBaseQueue.Len) | Whenever it decides whether there is work: before it pulls a job, for [`NumPending`](https://pkg.go.dev/github.com/goptics/varmq#Worker.NumPending), for [`Wait`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Wait), and for the `MaxLen` and `MinLen` [strategies](https://varmq.goptics.org/guides/strategies/). On a persistent or distributed queue each call is a call to the storage. |
| [`Dequeue() (any, bool)`](https://pkg.go.dev/github.com/goptics/varmq#IBaseQueue.Dequeue) | Once for each job it takes from a queue that does not acknowledge. It does not call it on a queue that is [`IAcknowledgeable`](#iacknowledgeable), which it reads with [`DequeueWithAckId`](https://pkg.go.dev/github.com/goptics/varmq#IAcknowledgeable.DequeueWithAckId). |
| [`Values() []any`](https://pkg.go.dev/github.com/goptics/varmq#IBaseQueue.Values) | Only when you call [`Purge`](https://pkg.go.dev/github.com/goptics/varmq#IBaseQueue.Purge) on the handle. VarMQ reads what is in the queue, clears it, then closes every value that implements [`io.Closer`](https://pkg.go.dev/io#Closer), which is how a purged job comes to read `Closed`. |
| [`Purge()`](https://pkg.go.dev/github.com/goptics/varmq#IBaseQueue.Purge) | When you call `Purge` on the handle, as above. |
| [`Close() error`](https://pkg.go.dev/github.com/goptics/varmq#IBaseQueue.Close) | When you call `Close` on the handle. |

On a handle from `WithDistributedQueue` or `NewDistributedQueue`, `Len`, `Purge` and `Close` are the adapter's own methods, called directly. VarMQ adds nothing around them, and `Purge` closes no jobs.

### `IQueue` and `IPriorityQueue`

[`IQueue`](https://pkg.go.dev/github.com/goptics/varmq#IQueue) adds [`Enqueue(item any) bool`](https://pkg.go.dev/github.com/goptics/varmq#IQueue.Enqueue) to `IBaseQueue`. [`IPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPriorityQueue) adds [`Enqueue(item any, priority int) bool`](https://pkg.go.dev/github.com/goptics/varmq#IPriorityQueue.Enqueue).

The handle's `Add` calls `Enqueue` once for each job, unless the job has an ID and the queue implements [`IDedupQueue`](#idedupqueue-and-ideduppriorityqueue), which it calls instead. `true` means the item is stored. `false` makes `Add` return `false`.

The priority is the number the caller passed to `Add`. VarMQ does not order the queue. The worker takes whatever `Dequeue` returns next. So a priority queue has to give out the lowest number first itself. For the built-in queues, and for redisq and sqliteq, a lower number comes first.

### `IAcknowledgeable`

The interface that makes a queue persistent. It has two methods:

| Method | The worker calls it |
|---|---|
| [`DequeueWithAckId() (any, bool, string)`](https://pkg.go.dev/github.com/goptics/varmq#IAcknowledgeable.DequeueWithAckId) | Once for each job it takes, when a slot is free and `Len` is above 0. It returns the item, whether there was one, and an acknowledgement ID. |
| [`Acknowledge(ackID string) bool`](https://pkg.go.dev/github.com/goptics/varmq#IAcknowledgeable.Acknowledge) | Once for each job, with the ID it was given, when the job's function has returned and the worker closes the job. It returns whether the storage accepted it. |

The worker keeps the ID on the job and passes it back unchanged. An empty ID means there is nothing to acknowledge, and the worker does not call `Acknowledge` for that job. `Acknowledge` returning `false` makes the worker report [`ErrAcknowledgeJob`](https://pkg.go.dev/github.com/goptics/varmq#ErrAcknowledgeJob). The storage decides what happens to an item that is taken and not acknowledged. It can give it out again after a timeout, at the next start, or never. So the delivery guarantee is the adapter's.

A worker takes one job per call. A job it took stays unacknowledged until it has run. If the worker held several, other consumers could not get them. The exception is the batch interface below.

### `IBatchAcknowledgeable`

An optional addition to `IAcknowledgeable`. [`IBatchAcknowledgeable`](https://pkg.go.dev/github.com/goptics/varmq#IBatchAcknowledgeable) has one more method, [`DequeueBatchWithAckIds(items []any, ackIDs []string) (n, remaining int, err error)`](https://pkg.go.dev/github.com/goptics/varmq#IBatchAcknowledgeable.DequeueBatchWithAckIds), for storage that can take many jobs in one call.

When the storage implements it and is the worker's only bound source, the worker reads it differently:

- **One call per wake-up.** The worker counts its free slots and calls `DequeueBatchWithAckIds` with buffers of that size, at most 64. It does not call `Len` between jobs.
- **Every job a call took starts.** The worker starts each job as it decodes it. It keeps none back, even if [`TunePool`](https://pkg.go.dev/github.com/goptics/varmq#Worker.TunePool) lowers the concurrency limit during the call. It also keeps none back if a pause or a stop arrives. The jobs are unacknowledged. If the worker kept them, a visibility timeout could give them to another consumer. A lower limit applies from the next call.
- **`n` is how many it moved.** The storage moves items into `items[:n]` and `ackIDs[:n]`, each item under its own ID, in one atomic step. It moves no more than the smaller of the two slice lengths. `n` is 0 with a `nil` error when the storage is empty.
- **`remaining` is the length after the move.** It may be a lower bound when the storage caps its count. It has to be above 0 whenever items remain, because the worker uses it to decide whether to call again.
- **A short call normally means the storage is empty.** The worker then stops pulling until something signals work. Storage may also return a short call with `remaining` above 0. For example, it can do this for items it gives back from an earlier failed call. The worker then pulls again.
- **`err` is a failure.** The worker reports it and pulls again on the next sign of work.
- **An item that does not decode is reported and not acknowledged.** The worker sends the error and leaves the item to the storage.

Signals matter because the worker does not probe the storage on its own after a short or failed call. A persistent queue is signalled by the process's own `Add` calls. Also, a worker that last saw the storage empty checks `Len` once a second. It does this while the worker is active and this is its only source. A distributed queue is signalled by [`Subscribe`](https://pkg.go.dev/github.com/goptics/varmq#ISubscribable.Subscribe). An adapter can implement both [`ISubscribable`](https://pkg.go.dev/github.com/goptics/varmq#ISubscribable) and this interface. Such an adapter has to send `"enqueued"` for items that no `Enqueue` announced:

- items a visibility timeout put back
- items another client added
- items left after a failed call

Sending it about once a second while items wait does that.

With another source bound as well, the worker reads this storage one job at a time through `DequeueWithAckId`, and checks `Len` before each read.

Binding a storage that implements it also signals the worker, because the storage may already hold jobs and no `Add` will announce them. So the [start-up trap](https://varmq.goptics.org/persistence/overview/#starting-a-worker-on-a-non-empty-queue) does not arise with it.

### `IDedupQueue` and [`IDedupPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDedupPriorityQueue)

[`IDedupQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDedupQueue) has [`EnqueueOnce(item any, id string) bool`](https://pkg.go.dev/github.com/goptics/varmq#IDedupQueue.EnqueueOnce). [`IDedupPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDedupPriorityQueue) has [`EnqueueOnce(item any, priority int, id string) bool`](https://pkg.go.dev/github.com/goptics/varmq#IDedupPriorityQueue.EnqueueOnce). Both are optional. `EnqueueOnce` stores the item unless an item with the same ID is already stored.

`Add` calls it, in place of `Enqueue`, for a job whose ID is not empty, when the storage implements it. Which IDs count depends on the queue:

- **On a persistent or distributed queue,** every ID counts, including one from a generator. Such storage keeps an ID for a window it sets. It returns `true` for a repeat too. So `Add` returns `true`, but the job is stored once. A persistent `Add` counts the repeat as submitted. That makes a retried `Add` safe after a lost reply. Use IDs that are unique across everything you add, such as a UUID, and not a business key or a counter that restarts.
- **On an in-memory queue bound with `WithQueue`,** only an ID you gave, with [`WithJobId`](https://pkg.go.dev/github.com/goptics/varmq#WithJobId) or [`Item.ID`](https://pkg.go.dev/github.com/goptics/varmq#Item), counts. The storage keeps an ID while its item is in the queue, until `Dequeue` or `Purge` takes it. A repeat returns `false`, and `Add` returns that, because a job that was not stored would not finish. See [job IDs](https://varmq.goptics.org/guides/job-ids/#deduplicating).

A producer made with `NewDistributedQueue` has no worker. The worker's [`WithJobIdGenerator`](https://pkg.go.dev/github.com/goptics/varmq#WithJobIdGenerator) does not reach it. It uses an ID you pass with `WithJobId`, or the one [`DefaultJobIdGenerator`](https://pkg.go.dev/github.com/goptics/varmq#DefaultJobIdGenerator) sets.

### `IPersistentQueue` and `IPersistentPriorityQueue`

[`IPersistentQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPersistentQueue) is `IQueue` plus `IAcknowledgeable`, and [`IPersistentPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPersistentPriorityQueue) is `IPriorityQueue` plus `IAcknowledgeable`. They add no methods. They are what `WithPersistentQueue` and `WithPersistentPriorityQueue` ask for.

Binding one wraps it so that `Add` writes JSON and the worker reads with `DequeueWithAckId`, or with `DequeueBatchWithAckIds` when the storage implements it. The worker takes one job per free slot. [`Pause`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Pause) and [`Stop`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Stop) do not park a job it already took.

### `ISubscribable`

[`ISubscribable`](https://pkg.go.dev/github.com/goptics/varmq#ISubscribable) has one method, [`Subscribe(func(action string))`](https://pkg.go.dev/github.com/goptics/varmq#ISubscribable.Subscribe). It is what makes a queue distributed.

`WithDistributedQueue` and `WithDistributedPriorityQueue` call `Subscribe` once, while binding, and pass the worker's handler. The adapter calls the handler with a string for each event. The worker acts on one: `"enqueued"` makes it wake to pull, and it also feeds the worker's counters. Any other action is ignored, so an adapter may send others. The handler runs on whichever goroutine the adapter calls it from, and it does not block. The interface has no way to unsubscribe, so the handler stays until the adapter stops calling it. `NewDistributedQueue` does not call `Subscribe`, because it has no worker.

For the counters of a worker on a persistent or distributed queue, see [the overview](https://varmq.goptics.org/persistence/overview/#metrics). [Metrics](https://varmq.goptics.org/guides/metrics/) covers in-memory queues.

### `IDistributedQueue` and `IDistributedPriorityQueue`

[`IDistributedQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDistributedQueue) is `IPersistentQueue` plus `ISubscribable`, and [`IDistributedPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDistributedPriorityQueue) is `IPersistentPriorityQueue` plus `ISubscribable`. They add no methods of their own.

### `CapacitySetter`

[`CapacitySetter`](https://pkg.go.dev/github.com/goptics/varmq#CapacitySetter) is optional and has one method, [`SetCapacity(cap int)`](https://pkg.go.dev/github.com/goptics/varmq#CapacitySetter.SetCapacity). VarMQ calls it when it creates a queue handle, with the number from [`WithQueueCapacity`](https://pkg.go.dev/github.com/goptics/varmq#WithQueueCapacity). It calls it even when you passed no capacity, with 0, so an implementation has to read a number below 1 as no limit. After that, it is the storage's job to refuse `Enqueue` when it is full.

Without it, [`IsFull`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue.IsFull) on the handle compares `Len` with the number, and `Add` is not refused. [Queues](https://varmq.goptics.org/guides/queues/#capacity) covers capacity.

## The handle and the job interfaces

The rest of the interfaces are not for adapters to implement. They describe what VarMQ gives you. They are listed here because they sit next to the queue interfaces in the API reference.

### [`IExternalBaseQueue`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue)

[`IExternalBaseQueue`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue) is what every queue handle has: `Len`, `IsFull`, `Purge` and `Close`, and a deprecated `NumPending` that does what `Len` does. VarMQ implements it around your storage. `Len`, `Purge` and `Close` reach the `IBaseQueue` methods above, and `IsFull` reads `Len`.

### Job interfaces

The job interfaces are small pieces that the job and group handles are built from. None of them is for storage to implement, because the jobs are VarMQ's own values.

| Interface | Methods | Where it appears |
|---|---|---|
| [`Identifiable`](https://pkg.go.dev/github.com/goptics/varmq#Identifiable) | [`ID() string`](https://pkg.go.dev/github.com/goptics/varmq#Identifiable.ID), empty when no ID was set | [`Job[T]`](https://pkg.go.dev/github.com/goptics/varmq#Job), the argument of the worker function, and every job handle |
| [`StatusProvider`](https://pkg.go.dev/github.com/goptics/varmq#StatusProvider) | [`IsClosed() bool`](https://pkg.go.dev/github.com/goptics/varmq#StatusProvider.IsClosed) and [`Status() string`](https://pkg.go.dev/github.com/goptics/varmq#StatusProvider.Status) | every job handle. The worker calls `IsClosed` on each job it takes from a queue and skips a closed one. |
| [`Awaitable`](https://pkg.go.dev/github.com/goptics/varmq#Awaitable) | [`Wait()`](https://pkg.go.dev/github.com/goptics/varmq#Awaitable.Wait), which blocks until the job is closed, or for a group until every job in it has ended | every job handle, and the group handles |
| [`Drainer`](https://pkg.go.dev/github.com/goptics/varmq#Drainer) | [`Drain()`](https://pkg.go.dev/github.com/goptics/varmq#Drainer.Drain), which discards the results or errors a group's channel still holds. A single job has no channel, so it does nothing there. | error and result job handles, and error and result group handles |
| [`PendingTracker`](https://pkg.go.dev/github.com/goptics/varmq#PendingTracker) | [`NumPending() int`](https://pkg.go.dev/github.com/goptics/varmq#PendingTracker.NumPending), the number of pending jobs in a group | the handles [`AddAll`](https://pkg.go.dev/github.com/goptics/varmq#Queue.AddAll) returns |

They belong to the in-memory queues. A persistent or distributed `Add` returns only a `bool`, so none of these reaches you from it. In the worker function of a persistent queue, `Job[T]` is still the argument, and its `ID()` returns the `id` stored in the JSON. [Jobs](https://varmq.goptics.org/concepts/jobs/) and [worker types](https://varmq.goptics.org/guides/worker-types/) cover the handles.

## What redisq and sqliteq implement

| Interface | redisq | sqliteq |
|---|---|---|
| `IBaseQueue`, `IQueue`, `IPriorityQueue`, `IAcknowledgeable` | yes | yes |
| `IPersistentQueue`, `IPersistentPriorityQueue` | yes | yes |
| `ISubscribable`, `IDistributedQueue`, `IDistributedPriorityQueue` | yes, on the `NewDistributed...` queues | no |
| `IBatchAcknowledgeable` | yes | yes |
| `IDedupQueue`, `IDedupPriorityQueue` | yes | yes |
| `CapacitySetter` | no | no |
