# Persistence and distribution

Persistent and distributed queues, when a job is acknowledged, and what VarMQ guarantees about delivery.

An in-memory queue is gone when the process exits. A persistent or distributed queue keeps its jobs in storage outside the process, behind a storage adapter. VarMQ has no adapter of its own. The two that exist are separate modules: [redisq](https://github.com/goptics/redisq) for Redis and [sqliteq](https://github.com/goptics/sqliteq) for SQLite.

This page covers what the two kinds of queue do in VarMQ. [Redis](https://varmq.goptics.org/persistence/redis/) and [SQLite](https://varmq.goptics.org/persistence/sqlite/) show the setup and what each adapter does with a job that was not finished. [Adapters](https://varmq.goptics.org/persistence/adapters/) lists the interfaces, for reading an adapter or writing one.

## Pick an adapter

- **[Redis](https://varmq.goptics.org/persistence/redis/) (redisq)**: use it when several processes share one queue, which is a distributed queue. It also gives persistent queues, with a Redis server to run.
- **[SQLite](https://varmq.goptics.org/persistence/sqlite/) (sqliteq)**: use it for one process with no server to run. It gives persistent queues only.

| Queue kind | redisq | sqliteq |
|---|---|---|
| Persistent queue and persistent priority queue | yes | yes |
| Distributed queue and distributed priority queue | yes, on the `NewDistributed...` queues | no |
| Store a retried `Add` once ([`IDedupQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDedupQueue)) | yes | yes |

[Adapters](https://varmq.goptics.org/persistence/adapters/#what-redisq-and-sqliteq-implement) has the full list of interfaces.

## Guarantees at a glance

| Question | Answer |
|---|---|
| How often does a job run? | At least once with redisq and sqliteq. A custom adapter decides, and it can lose a job: see [what this guarantees](#what-this-guarantees). A job can run twice, so write the job function so that a second run is safe. |
| Does VarMQ retry a failed job? | No. A job that fails or panics is acknowledged like any other. To acknowledge a job is to tell the storage that the job is done, so the storage forgets it. |
| In what order do jobs leave storage? | In the order the adapter gives: first in, first out, or lowest priority number first. |
| Is a repeated `Add` stored once? | Only with [`WithJobId`](https://pkg.go.dev/github.com/goptics/varmq#WithJobId) and an adapter that implements [`IDedupQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDedupQueue). redisq and sqliteq do. |

[What this guarantees](#what-this-guarantees) explains each answer.

## Persistent or distributed

You bind both kinds to a worker the way you bind an in-memory queue. Only a plain worker from [`NewWorker`](https://pkg.go.dev/github.com/goptics/varmq#NewWorker) can bind them. An error worker or a result worker binds in-memory queues only.

| | Persistent | Distributed |
|---|---|---|
| Bind with | [`WithPersistentQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithPersistentQueue), [`WithPersistentPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithPersistentPriorityQueue) | [`WithDistributedQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithDistributedQueue), [`WithDistributedPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithDistributedPriorityQueue) |
| The storage must implement | [`IPersistentQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPersistentQueue), [`IPersistentPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IPersistentPriorityQueue) | [`IDistributedQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDistributedQueue), [`IDistributedPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDistributedPriorityQueue): the persistent ones plus [`ISubscribable`](https://pkg.go.dev/github.com/goptics/varmq#ISubscribable) |
| You get back | [`PersistentQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#PersistentQueue) or [`PersistentPriorityQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#PersistentPriorityQueue), which has [`Worker()`](https://pkg.go.dev/github.com/goptics/varmq#PersistentQueue.Worker) | [`DistributedQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#DistributedQueue) or [`DistributedPriorityQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#DistributedPriorityQueue), which has no `Worker()` |
| Jobs survive a restart | as long as the storage keeps them | as long as the storage keeps them |
| The worker learns of new jobs | from its own process, plus a check once a second in some cases | from its own process, and from the storage |
| A process that only adds jobs | not provided | [`NewDistributedQueue`](https://pkg.go.dev/github.com/goptics/varmq#NewDistributedQueue) and [`NewDistributedPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#NewDistributedPriorityQueue) wrap the storage with no worker |

The row about learning of new jobs is the one that decides which to use. A persistent worker pulls when something in its own process signals it: an [`Add`](https://pkg.go.dev/github.com/goptics/varmq#PersistentQueue.Add), a start or resume, or one of its own jobs finishing. Sometimes redisq or sqliteq is the worker's only source. Then the worker also checks the storage once a second, while it is active and last found the storage empty. So it finds a job that another process added within about a second. With another source bound as well, nothing signals the worker when another process adds a job. So it may not look until one of those signals happens. [Adapters](https://varmq.goptics.org/persistence/adapters/#ibatchacknowledgeable) has the details. Use a persistent queue when one process wants its jobs to survive a restart.

A distributed queue adds [`Subscribe`](https://pkg.go.dev/github.com/goptics/varmq#ISubscribable.Subscribe). The worker subscribes when you bind the queue, and the adapter calls the worker back with the action `"enqueued"` when a job is added. That wakes the worker to pull. It is how several processes can consume one queue, with each job going to one of them. [Adapters](https://varmq.goptics.org/persistence/adapters/#isubscribable) has the rest of the contract.

## What is stored

VarMQ stores each job as JSON, in the shape `{"id":"","status":"Created","data":"job-1"}`. `data` is what you passed to `Add`. That has two consequences:

- **The payload type has to work with `encoding/json`.** `Add` returns `false` when JSON encoding fails, for example for a function. So you cannot use [function helpers](https://varmq.goptics.org/guides/function-helpers/) here.
- **Producers and consumers have to agree on the type.** A consumer decodes the stored JSON into its own `T`. Bytes that do not decode are reported as [`ErrParseJob`](https://pkg.go.dev/github.com/goptics/varmq#ErrParseJob) on [`Errs()`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Errs) and to the [error handler](https://varmq.goptics.org/guides/errors/#the-error-handler). The worker does not acknowledge such a job, so the adapter decides what happens to it next.

`Add` on these queues returns only a `bool`. It is `false` when the encoding fails or when the storage refuses the job, for example because the queue was closed. There is no job handle, so there is nothing to [`Wait`](https://pkg.go.dev/github.com/goptics/varmq#Awaitable.Wait) on and no [`Err`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedErrJob.Err) or [`Result`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedResultJob.Result) to read, and there is no [`AddAll`](https://pkg.go.dev/github.com/goptics/varmq#Queue.AddAll). [Worker types](https://varmq.goptics.org/guides/worker-types/#which-queues-each-worker-can-bind) has the table.

The handle's [`Len`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue.Len) asks the storage, so it counts the jobs waiting there, including jobs another process added. [`WithQueueCapacity`](https://pkg.go.dev/github.com/goptics/varmq#WithQueueCapacity) reaches the storage only if the adapter implements [`CapacitySetter`](https://pkg.go.dev/github.com/goptics/varmq#CapacitySetter). Neither redisq nor sqliteq does. With them `Add` is not refused, and [`IsFull`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue.IsFull) only compares `Len` with the number you passed.

## Acknowledgement

An in-memory job is done when its function returns. A job in storage needs one more step, so that storage knows the job was not lost on the way.

1. When the worker has a free slot, it takes a job with [`DequeueWithAckId`](https://pkg.go.dev/github.com/goptics/varmq#IAcknowledgeable.DequeueWithAckId). Some queues are the worker's only source and implement [`IBatchAcknowledgeable`](https://pkg.go.dev/github.com/goptics/varmq#IBatchAcknowledgeable), as redisq and sqliteq do. For such a queue, the worker takes a job for each free slot in one [`DequeueBatchWithAckIds`](https://pkg.go.dev/github.com/goptics/varmq#IBatchAcknowledgeable.DequeueBatchWithAckIds) call instead. The storage gives each job together with an acknowledgement ID. It keeps its own record that the job is out.
2. The job's function runs.
3. When the function returns, the worker closes the job, and closing it calls [`Acknowledge`](https://pkg.go.dev/github.com/goptics/varmq#IAcknowledgeable.Acknowledge) with that ID. The storage can now forget the job.

Three details about step 3:

- **A failed or panicking job is acknowledged too.** VarMQ recovers a panic, counts the job as failed and reports the panic. Then it acknowledges the job like any other. Nothing in VarMQ retries a job.
- **A job whose acknowledgement fails is reported.** If `Acknowledge` returns `false`, the worker sends [`ErrAcknowledgeJob`](https://pkg.go.dev/github.com/goptics/varmq#ErrAcknowledgeJob) to `Errs()` and the handler. The job's function has already run.
- **Taken jobs run.** A worker with these queues takes one job per free slot and starts it. A [`Pause`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Pause) or [`Stop`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Stop) does not park a job the worker already took. The job is unacknowledged, and after a long wait a visibility timeout could give it to another consumer. A visibility timeout is the time some storage, such as redisq, waits for an acknowledgement. After that time, the storage gives the job out again. See [shutting down](#shutting-down).

The scene below runs one job through these steps. Pick where the queue lives, then press **Kill the process** at any step. It shows what the storage holds. It also shows what redisq, sqliteq or your own adapter does with a job that was taken and not acknowledged.

## What this guarantees

VarMQ acknowledges a job it took from storage after the job's function has returned, and not before. So if the process dies while a job runs, the job was not acknowledged.

What the storage then does with it is the adapter's decision. VarMQ cannot make it come back. [Redis](https://varmq.goptics.org/persistence/redis/#jobs-that-were-not-acknowledged) and [SQLite](https://varmq.goptics.org/persistence/sqlite/#jobs-that-were-not-acknowledged) say what redisq and sqliteq do. With either adapter the best case is at-least-once delivery:

- **A job can run twice.** With these adapters, a job is given out again if its process died while it ran. A job is also given out again if its acknowledgement failed after it ran. Write the job function so that running it twice is safe.
- **A job can be lost with some adapters.** Some adapters take a job and record it as in progress in two separate steps. Such an adapter can lose the job if the process dies between the steps. redisq and sqliteq do both in one step.
- **VarMQ does not deduplicate for you.** [`WithJobId`](https://pkg.go.dev/github.com/goptics/varmq#WithJobId) puts an ID in the stored JSON. Storage that implements [`IDedupQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDedupQueue) can use it to store a retried `Add` once. This stops a double add. It says nothing about a double run. redisq and sqliteq both implement it.

## Starting a worker on a non-empty queue

After a restart, jobs from the earlier run are still in storage. Bind the queue first and start the worker afterwards:

```go
worker := varmq.NewWorker(process, varmq.WithAutoRun(false))
queue := worker.WithPersistentQueue(pq)
worker.Start()
```

A worker that starts on creation, which is the default, signals its event loop to look for work before you have bound the queue. If the loop reads that signal first, it finds no queue. Some adapters signal on bind, as below. Without that, nothing signals the loop again until something in this process does, as listed above. Until then, the old jobs can stay in storage. With [`WithAutoRun(false)`](https://pkg.go.dev/github.com/goptics/varmq#WithAutoRun), [`Start`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Start) runs after the bind and signals the loop itself. [Lifecycle](https://varmq.goptics.org/concepts/lifecycle/#withautorun) covers the option.

Adapters that implement [`IBatchAcknowledgeable`](https://pkg.go.dev/github.com/goptics/varmq#IBatchAcknowledgeable) signal the worker when you bind them. redisq and sqliteq both do, so with them the order does not matter.

## Shutting down

Close things in this order, so that every running job can acknowledge:

1. Stop adding jobs.
2. Call [`StopAndWait`](https://pkg.go.dev/github.com/goptics/varmq#Worker.StopAndWait) on the worker. Running jobs finish and acknowledge. To run every waiting job first, call [`WaitAndStop`](https://pkg.go.dev/github.com/goptics/varmq#Worker.WaitAndStop), then `WaitUntilStopped`.
3. Call `Close` on the adapter's queue. With redisq, `Close` puts back every job that queue object took and did not acknowledge, so a job that still runs would run again.
4. Call `Close` on the adapter's `Queues` value.

With `defer`, the order is the reverse of the calls. Defer the `Queues` close first, then the queue close. The programs on [Redis](https://varmq.goptics.org/persistence/redis/) and [SQLite](https://varmq.goptics.org/persistence/sqlite/) do this.

## Metrics

[Metrics](https://varmq.goptics.org/guides/metrics/) describes the four counters for in-memory queues. For these queues, keep three things in mind.

- **[`Submitted`](https://pkg.go.dev/github.com/goptics/varmq#Metrics.Submitted) on a persistent queue counts this process's accepted `Add` calls.** It does not count a job that was already in storage. It also does not count a job that another process added. It still counts in [`Completed`](https://pkg.go.dev/github.com/goptics/varmq#Metrics.Completed) when this worker runs it, so `Completed` can be higher than `Submitted`.
- **The counters of a worker on a distributed queue may differ from what you expect.** `Submitted` in particular. These docs do not cover how a distributed queue's counters count yet.
- **[`NumPending`](https://pkg.go.dev/github.com/goptics/varmq#Worker.NumPending) asks the storage.** It is the number of jobs waiting there, plus jobs the worker has taken and not yet started. The queue handle's `Len` leaves out the second kind. Each call is a call to the storage.

## Topics

[Topics](https://varmq.goptics.org/guides/topics/) are in-memory only. To share jobs between processes, use a distributed queue.
