# Worker types

NewWorker, NewErrWorker and NewResultWorker compared: what each returns, what Add gives back, and which queues each can bind.

VarMQ has three worker constructors. In each, your worker function receives a [`Job[T]`](https://pkg.go.dev/github.com/goptics/varmq#Job), where `T` is the type of the data you pass to [`Add`](https://pkg.go.dev/github.com/goptics/varmq#Queue.Add). They differ in what the function returns. That decides what `Add` hands back, and how you find out what happened to a job.

| Constructor | Your function | `Add` returns | Read the outcome with |
|---|---|---|---|
| [`NewWorker`](https://pkg.go.dev/github.com/goptics/varmq#NewWorker) | `func(j Job[T])` | [`EnqueuedJob`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedJob) | nothing to read |
| [`NewErrWorker`](https://pkg.go.dev/github.com/goptics/varmq#NewErrWorker) | `func(j Job[T]) error` | [`EnqueuedErrJob`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedErrJob) | [`Err() error`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedErrJob.Err) |
| [`NewResultWorker`](https://pkg.go.dev/github.com/goptics/varmq#NewResultWorker) | `func(j Job[T]) (R, error)` | [`EnqueuedResultJob[R]`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedResultJob) | [`Result() (R, error)`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedResultJob.Result) |

Pick a constructor and a job below. The same function runs under all three, so you see what changes: the type `Add` hands back, and where a failure appears.

`R` is the type of the result. `Add` returns the handle together with a `bool`, `false` when the queue did not take the job. [Jobs](https://varmq.goptics.org/concepts/jobs/#two-views-of-a-job) covers both.

## A plain worker

`NewWorker` is for work that has nothing to report back. The handle lets you [`Wait`](https://pkg.go.dev/github.com/goptics/varmq#Awaitable.Wait) for the job, read its status or close it while it waits, but it carries no outcome.

```go
package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println("worker got", j.Data())
	})
	queue := worker.BindQueue()

	job, _ := queue.Add("hello")
	job.Wait() // blocks until the job is done; there is no outcome to read
	fmt.Println("job done")
}
```

The worker's function prints `worker got hello`. `Wait` returns once the job is done, and there is nothing else to read from the handle.

If the function panics, the worker recovers and counts the job as failed. It reports the panic on the worker's [`Errs()`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Errs) channel and to the handler you set with [`WithErrHandler`](https://pkg.go.dev/github.com/goptics/varmq#WithErrHandler). The handle does not get it. [Errors](https://varmq.goptics.org/guides/errors/) covers both.

## An error worker

`NewErrWorker` takes a function that returns an `error`. `Err()` blocks until the job is closed, then returns what the function returned: `nil` when it succeeded. A panic comes back through `Err()` as an error. The worker reports the same error on its own `Errs()` channel and to its error handler too, so one failure appears in both places.

A job that was closed before it ran also releases `Err`, with `nil`. So `nil` means the function reported no error, not that it ran. See [closing a queued job](https://varmq.goptics.org/concepts/jobs/#closing-a-queued-job).

```go
package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		if j.Data()%2 != 0 {
			return fmt.Errorf("%d is odd", j.Data())
		}
		return nil
	})
	queue := worker.BindQueue()

	for _, n := range []int{2, 3} {
		job, _ := queue.Add(n)
		fmt.Printf("job %d: %v\n", n, job.Err())
	}
}
```

`Err` returned what the function returned for each job: `<nil>` for 2 and `3 is odd` for 3. The program reads each outcome right after `Add` to stay short. In a real program you usually add many jobs first and read the outcomes later.

## A result worker

`NewResultWorker` takes a function that returns `(R, error)`. `Result()` blocks until the job is closed, then returns both.

When the function returns a non-nil error, `Result` returns that error and the zero value of `R`, whatever value the function returned beside it. A panic does the same: the zero value, and the panic as the error. As with an error worker, the error also reaches `Errs()` and the error handler.

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
	})
	queue := worker.BindQueue()

	for _, s := range []string{"42", "forty-two"} {
		job, _ := queue.Add(s)
		n, err := job.Result()
		fmt.Printf("%q: %d, %v\n", s, n, err)
	}
}
```

`"42"` gives the result 42 and no error. `"forty-two"` gives the zero value of `int` and the error from `strconv.Atoi`.

## What a handle has

Every handle has [`ID()`](https://pkg.go.dev/github.com/goptics/varmq#Identifiable.ID), [`Status()`](https://pkg.go.dev/github.com/goptics/varmq#StatusProvider.Status), [`IsClosed()`](https://pkg.go.dev/github.com/goptics/varmq#StatusProvider.IsClosed), `Wait()` and [`Close()`](https://pkg.go.dev/github.com/goptics/varmq#EnqueuedJob.Close). The error and result handles add `Err()` or `Result()`, and a [`Drain()`](https://pkg.go.dev/github.com/goptics/varmq#Drainer.Drain) method.

`Drain` does nothing on a single job. It exists for the group handles that [`AddAll`](https://pkg.go.dev/github.com/goptics/varmq#Queue.AddAll) returns ([adding many jobs](https://varmq.goptics.org/guides/adding-many/)). So `Err` and `Result` give the same answer whether or not you call it.

## Which queues each worker can bind

Each constructor returns a binder that holds the binding methods: [`IWorkerBinder[T]`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder) from `NewWorker`, [`IErrWorkerBinder[T]`](https://pkg.go.dev/github.com/goptics/varmq#IErrWorkerBinder) from `NewErrWorker` and [`IResultWorkerBinder[T, R]`](https://pkg.go.dev/github.com/goptics/varmq#IResultWorkerBinder) from `NewResultWorker`. The in-memory binding methods return a queue handle whose type follows the worker.

| | `NewWorker` | `NewErrWorker` | `NewResultWorker` |
|---|---|---|---|
| [`BindQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.BindQueue) and [`WithQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithQueue) return | [`Queue[T]`](https://pkg.go.dev/github.com/goptics/varmq#Queue) | [`ErrQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#ErrQueue) | [`ResultQueue[T, R]`](https://pkg.go.dev/github.com/goptics/varmq#ResultQueue) |
| [`BindPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.BindPriorityQueue) and [`WithPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithPriorityQueue) return | [`PriorityQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#PriorityQueue) | [`ErrPriorityQueue[T]`](https://pkg.go.dev/github.com/goptics/varmq#ErrPriorityQueue) | [`ResultPriorityQueue[T, R]`](https://pkg.go.dev/github.com/goptics/varmq#ResultPriorityQueue) |
| [`WithPersistentQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithPersistentQueue), [`WithPersistentPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithPersistentPriorityQueue), [`WithDistributedQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithDistributedQueue), [`WithDistributedPriorityQueue`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithDistributedPriorityQueue) | yes | no | no |
| [`WithTopic`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithTopic) | yes | no | no |

An error or result worker binds in-memory queues only. For a persistent queue, a distributed queue or a topic, use `NewWorker`.

The handles in this guide are what an in-memory queue's `Add` returns. On a persistent or distributed queue, `Add` returns only a `bool`, `false` when the storage refused the job. There is no job handle, so there is nothing to `Wait` on or to read an outcome from.

## What follows the function

All three constructors take the same trailing arguments, `config ...any`: an `int` for the concurrency, a `string` for the name, and option functions such as [`WithAutoRun`](https://pkg.go.dev/github.com/goptics/varmq#WithAutoRun). [Configuration](https://varmq.goptics.org/guides/configuration/#options-and-positional-arguments) has the rules.

```go
worker := varmq.NewResultWorker(parse, 4, "parser", varmq.WithAutoRun(false))
```
