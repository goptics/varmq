# Adding many jobs

AddAll queues a slice of items in one call and returns one handle for the whole group. Wait for it, count what is pending, and read results or errors as they finish.

[`AddAll`](https://pkg.go.dev/github.com/goptics/varmq#Queue.AddAll) queues a slice of [`Item`](https://pkg.go.dev/github.com/goptics/varmq#Item) values in one call. It returns one handle for the whole group, not one per job. It is a method of the in-memory queues: persistent and distributed queues have only [`Add`](https://pkg.go.dev/github.com/goptics/varmq#Queue.Add).

An `Item` has three fields:

| Field | Meaning |
|---|---|
| [`Data`](https://pkg.go.dev/github.com/goptics/varmq#Item) | What you would pass to `Add`. |
| [`ID`](https://pkg.go.dev/github.com/goptics/varmq#Item) | An optional job ID. The worker sees it with a `g:` prefix. [Job IDs](https://varmq.goptics.org/guides/job-ids/) has the rules. |
| [`Priority`](https://pkg.go.dev/github.com/goptics/varmq#Item) | Read by a priority queue only. A FIFO queue ignores it. A lower number runs first, as in [queues](https://varmq.goptics.org/guides/queues/). |

```go
items := []varmq.Item[string]{
	{ID: "a", Data: "10"},
	{ID: "b", Data: "x"},
}
group := queue.AddAll(items)
```

## The group handle

The handle's type follows the worker, as it does for `Add`. [Worker types](https://varmq.goptics.org/guides/worker-types/) compares the three.

| Worker | `AddAll` returns | What you read |
|---|---|---|
| [`NewWorker`](https://pkg.go.dev/github.com/goptics/varmq#NewWorker) | [`EnqueuedGroupJob`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedGroupJob) | nothing, you only wait |
| [`NewErrWorker`](https://pkg.go.dev/github.com/goptics/varmq#NewErrWorker) | [`EnqueuedErrGroupJob`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedErrGroupJob) | [`Errs()`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedErrGroupJob.Errs) |
| [`NewResultWorker`](https://pkg.go.dev/github.com/goptics/varmq#NewResultWorker) | [`EnqueuedResultGroupJob[R]`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedResultGroupJob) | [`Results()`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedResultGroupJob.Results) |

All three have [`NumPending()`](https://pkg.go.dev/github.com/goptics/varmq#PendingTracker.NumPending) and [`Wait()`](https://pkg.go.dev/github.com/goptics/varmq#Awaitable.Wait). The error and result handles also have [`Drain()`](https://pkg.go.dev/github.com/goptics/varmq#Drainer.Drain).

- `NumPending` is the number of jobs in the group that have not ended. A job ends when it runs, or when it is closed without running, by [`Purge`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue.Purge) for example. The number counts down to 0.
- `Wait` blocks until `NumPending` is 0. On a plain worker, `group.Wait()` is all you need.

There is no [`Close`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedJob.Close) on a group handle, and no status for a single item. To close one job, add it with `Add` instead, which gives you a handle of its own.

## Items the queue refuses

The scene shows the same items added both ways. Add the items, then start the worker.

`AddAll` adds the items one at a time, and skips each one the queue does not take: the queue is full or closed, or the item's ID belongs to a job still in the queue. That includes a repeated ID inside the same slice, while the first copy is still in the queue (a running worker may already have taken it). A skipped item never runs and sends nothing to `Results()` or `Errs()`. It does not count in `NumPending`, so `NumPending` can start below `len(items)`.

`AddAll` does not say which items it skipped. If you need to know, call `Add` for each job: it returns a `bool` per job.

The worker below is created with [`WithAutoRun(false)`](https://pkg.go.dev/github.com/goptics/varmq#WithAutoRun), so you can look at the group before any job runs. The third item repeats the ID `a`.

```go
package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// Not started yet, so we can look at the group before any job runs.
	worker := varmq.NewWorker(
		func(j varmq.Job[string]) {
			fmt.Println("running", j.ID(), j.Data())
		},
		varmq.WithAutoRun(false),
	)
	queue := worker.BindQueue()

	items := []varmq.Item[string]{
		{ID: "a", Data: "resize"},
		{ID: "b", Data: "upload"},
		{ID: "a", Data: "resize again"}, // "a" is still in the queue: skipped
	}
	group := queue.AddAll(items)
	fmt.Println("pending:", group.NumPending(), "of", len(items))

	worker.Start()
	group.Wait()
	fmt.Println("pending:", group.NumPending())
}
```

`pending: 2 of 3` shows the repeat was skipped. After [`Start`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Start), only `g:a` and `g:b` run, `Wait` returns, and `NumPending` is 0.

## Reading results

`Results()` returns a channel of [`Result[R]`](https://pkg.go.dev/github.com/goptics/varmq#Result) values. It returns at once. Ranging over the channel is what waits.

```go
package main

import (
	"fmt"
	"strconv"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewResultWorker(func(j varmq.Job[string]) (int, error) {
		return strconv.Atoi(j.Data())
	}, 3)
	queue := worker.BindQueue()

	group := queue.AddAll([]varmq.Item[string]{
		{ID: "a", Data: "10"},
		{ID: "b", Data: "x"},
		{ID: "c", Data: "30"},
	})

	for r := range group.Results() {
		if r.Err != nil {
			fmt.Println(r.JobId, "failed:", r.Err)
			continue
		}
		fmt.Println(r.JobId, "=", r.Data)
	}
	fmt.Println("pending:", group.NumPending())
}
```

The worker has concurrency 3, so the three result lines can come in a different order on each run. `g:b` fails to parse and arrives as an `Err`. `pending: 0` prints after the loop ends by itself.

- A `Result` has a [`JobId`](https://pkg.go.dev/github.com/goptics/varmq#Result), a [`Data`](https://pkg.go.dev/github.com/goptics/varmq#Result) value and an [`Err`](https://pkg.go.dev/github.com/goptics/varmq#Result). A job that succeeded has `Data`. One that failed has `Err`, and `Data` is the zero value. A panic arrives as an `Err`.
- `JobId` is the item's ID with the `g:` prefix. It is empty for an item with no ID, unless the worker has a [job ID generator](https://varmq.goptics.org/guides/job-ids/#giving-a-job-an-id).
- Results arrive in the order the jobs finish. With concurrency above 1 that is not the order of your slice, so match results to items by `JobId`. With concurrency 1 on a FIFO queue they come back in the order you added them.
- A job closed before it ran sends nothing.
- The channel closes once every job in the group has ended, so the `range` loop finishes by itself.
- The channel has room for one value per item. A slow reader does not hold up the workers.

## Reading errors

`Errs()` returns a channel of `error`. It carries failures only: a job that succeeded sends nothing, so the number of values you receive is the number of jobs that failed. A panic counts as a failure. The channel closes once every job has ended, so ranging over it finishes by itself.

An error does not say which job it came from. If you need that, put the job's ID in the message, as the worker function below does with [`j.ID()`](https://pkg.go.dev/github.com/goptics/varmq#Identifiable.ID).

```go
package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		if j.Data() < 0 {
			// An error does not name its job, so put the ID in the message.
			return fmt.Errorf("%s: %d is negative", j.ID(), j.Data())
		}
		return nil
	})
	queue := worker.BindQueue()

	group := queue.AddAll([]varmq.Item[int]{
		{ID: "p", Data: 5},
		{ID: "q", Data: -1},
		{ID: "r", Data: 7},
		{ID: "s", Data: -2},
	})

	failed := 0
	for err := range group.Errs() {
		fmt.Println(err)
		failed++
	}
	fmt.Println(failed, "of 4 failed")
}
```

Two of the four items are negative, so the loop receives two errors, `g:q` and `g:s`. The jobs that succeeded sent nothing.

## If you do not read the channel

[`Drain()`](https://pkg.go.dev/github.com/goptics/varmq#Drainer.Drain) discards the results or errors the channel still holds, for a caller that is not going to read them.

:::caution
Do not range over `Results()` or `Errs()` after calling `AddAll` with an empty slice. With no items, no job ever ends, so nothing closes the channel and the loop never finishes. `Wait()` returns at once in that case. Check `len(items) > 0` before you read.
:::
