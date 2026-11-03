# Job IDs

Give a job an ID with WithJobId, Item.ID or a generator, and use IDs to keep at most one job per ID in an in-memory queue.

A job has no ID unless you give it one: [`ID()`](https://pkg.go.dev/github.com/goptics/varmq#Identifiable.ID) returns an empty string. An ID names the job for your own bookkeeping. On an in-memory queue it also keeps a second job with the same ID from waiting in the queue next to the first. [Jobs](https://varmq.goptics.org/concepts/jobs/#job-ids) introduces IDs. This page has the details.

## Giving a job an ID

There are three ways.

| How | Where | The job gets |
|---|---|---|
| [`WithJobId("job-1")`](https://pkg.go.dev/github.com/goptics/varmq#WithJobId) | an option you pass to [`Add`](https://pkg.go.dev/github.com/goptics/varmq#Queue.Add) | that ID. An empty string is ignored. |
| [`Item.ID`](https://pkg.go.dev/github.com/goptics/varmq#Item) | a field of the `Item` values you pass to [`AddAll`](https://pkg.go.dev/github.com/goptics/varmq#Queue.AddAll) | that ID with a `g:` prefix. |
| [`WithJobIdGenerator(fn)`](https://pkg.go.dev/github.com/goptics/varmq#WithJobIdGenerator) | an option you pass to the worker | whatever `fn` returns, when no ID was given. |

The generator is a `func() string`. Passing `nil` is ignored. Without a generator, a job with no ID of its own has none. [`DefaultJobIdGenerator`](https://pkg.go.dev/github.com/goptics/varmq#DefaultJobIdGenerator) sets a generator for the workers you create after the call.

A few details:

- An ID from `WithJobId` or `Item.ID` takes the place of the generated one. `Add` still calls the generator when you pass `WithJobId`, and discards its value, so a counting generator skips a number. `AddAll` calls it only for items that have no ID.
- A generated ID on an `AddAll` item gets the `g:` prefix too.
- `Add` calls the generator on the goroutine that calls `Add`. If several goroutines add jobs, the generator has to be safe for concurrent use.

The worker below has a counting generator. It gets one job with no ID, one with `WithJobId`, and two `AddAll` items, one with an ID and one without.

```go
package main

import (
	"fmt"
	"sync/atomic"

	"github.com/goptics/varmq"
)

func main() {
	var next atomic.Int32

	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Printf("%s has id %q\n", j.Data(), j.ID())
	}, varmq.WithJobIdGenerator(func() string {
		return fmt.Sprintf("auto-%d", next.Add(1))
	}))
	queue := worker.BindQueue()

	queue.Add("report")
	queue.Add("invoice", varmq.WithJobId("invoice-7"))
	queue.AddAll([]varmq.Item[string]{
		{ID: "a", Data: "item a"},
		{Data: "item b"},
	})

	worker.WaitUntilIdle()
}
```

`report` gets `auto-1` from the generator, and `invoice` keeps its own ID. `item a` appears as `g:a`. `item b` gets `g:auto-3`: the prefix applies to a generated ID too, and `auto-2` went to the `invoice` call and was discarded.

## Where you see the ID

- In the worker function, as `j.ID()` on the [`Job`](https://pkg.go.dev/github.com/goptics/varmq#Job) it receives.
- On the handle `Add` returns: `job.ID()`.
- In each [`Result`](https://pkg.go.dev/github.com/goptics/varmq#Result) that [`Results()`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedResultGroupJob.Results) delivers for an `AddAll` group on a result worker, in its [`JobId`](https://pkg.go.dev/github.com/goptics/varmq#Result) field. See [adding many jobs](https://varmq.goptics.org/guides/adding-many/).

It is the empty string when the job has no ID.

## Deduplicating

An in-memory queue keeps at most one job per given ID in the queue. While a job with an ID is in the queue, the queue refuses a second `Add` with the same ID. That `Add` returns a `nil` handle and `false`.

The worker below is created with [`WithAutoRun(false)`](https://pkg.go.dev/github.com/goptics/varmq#WithAutoRun), so the jobs wait in the queue until [`Start`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Start). On a running worker, the first job could leave the queue before the second `Add`, which releases its ID and lets the second one in.

```go
package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// Not started, so the jobs stay in the queue.
	worker := varmq.NewWorker(
		func(j varmq.Job[string]) {
			fmt.Println("running", j.Data())
		},
		varmq.WithAutoRun(false),
	)
	queue := worker.BindQueue()

	_, ok := queue.Add("refresh 1", varmq.WithJobId("refresh"))
	fmt.Println("first:", ok)

	job, ok := queue.Add("refresh 2", varmq.WithJobId("refresh"))
	fmt.Println("second:", ok, "- handle is nil:", job == nil)

	// AddAll prefixes the ID, so this is "g:refresh" and does not collide.
	group := queue.AddAll([]varmq.Item[string]{{ID: "refresh", Data: "refresh 3"}})
	fmt.Println("AddAll item queued:", group.NumPending())

	worker.Start()
	worker.WaitUntilIdle()

	// The first job has left the queue, so the ID is free again.
	job, ok = queue.Add("refresh 4", varmq.WithJobId("refresh"))
	job.Wait()
	fmt.Println("accepted again:", ok)
}
```

The second `Add` with the ID `refresh` is refused and returns no handle, so `refresh 2` never runs. The `AddAll` item is accepted, because its ID is `g:refresh`. Once the jobs have left the queue, `refresh` can be used again.

That suits work where many callers ask for the same thing, and one waiting job is enough.

### When an ID is free

The queue holds an ID while its job is in the queue. The ID is released when a worker takes the job out of the queue, or when [`Purge`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue.Purge) removes it.

A worker takes jobs out of the queue before it runs them. With a FIFO queue, it may take several at once in a batch ([how it works](https://varmq.goptics.org/concepts/how-it-works/#batched-dispatch)). A job in a batch, or one parked by a [pause or stop](https://varmq.goptics.org/concepts/lifecycle/#what-happens-to-jobs), has left the queue, so it has released its ID. Its [status](https://varmq.goptics.org/concepts/jobs/#job-status) still reads `Queued`. A duplicate `Add` is accepted at that point.

So the rule is one job per ID *in the queue*, not one live job per ID. It does not stop the same ID from running twice at once: when a worker has taken the first job, the ID is free.

Closing a job with its handle's [`Close`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedJob.Close) does not free its ID. `Close` leaves the job in the queue, so it keeps the ID until the worker reaches it or `Purge` removes it. See [closing a queued job](https://varmq.goptics.org/concepts/jobs/#closing-a-queued-job).

### What is checked

- Only IDs you gave, with `WithJobId` or `Item.ID`. A generated ID is not checked, and two jobs with no ID are both stored.
- `AddAll` applies the same rule per item. `AddAll` skips an item whose ID belongs to a job in the queue. It also skips a repeat within the same slice while the first copy is still in the queue. A running worker may already have taken that first copy. Items get a `g:` prefix, so an item with ID `a` and an `Add` with `WithJobId("a")` do not collide.
- Every in-memory queue is checked: FIFO and priority queues, and their error and result variants.
- A queue you bind with [`WithQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithQueue) or [`WithPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithPriorityQueue) is checked only if it implements [`IDedupQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDedupQueue) or [`IDedupPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IDedupPriorityQueue). Without that, the queue stores the duplicate.

`Add` also returns `false` for a full queue and for a closed one, and it does not say which of the three it was. See [queues](https://varmq.goptics.org/guides/queues/).

### Persistent and distributed queues

Persistent and distributed queues do not work this way. Storage that implements `IDedupQueue` keeps an ID for a window that the storage itself sets. `Add` returns `true` for a repeat, but the storage keeps the job only once. IDs there must be unique across everything you add. Use something like a UUID, not a counter that restarts. The `IDedupQueue` documentation has the details.
