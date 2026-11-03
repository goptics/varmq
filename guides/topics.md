# Topics

Publish once, and every subscribed worker gets its own job. Topics are in-memory only.

A queue gives each job to one worker. A [`Topic`](https://pkg.go.dev/github.com/goptics/varmq#Topic) gives each payload to every worker subscribed to it. Use one when several workers must each act on the same event, such as sending an email and updating stock for the same order.

| | In-memory queue | Topic |
|---|---|---|
| Who gets a job | one worker, once | every subscribed worker, each with its own job |
| You send with | [`queue.Add(x)`](https://pkg.go.dev/github.com/goptics/varmq#Queue.Add) | [`topic.Publish(x)`](https://pkg.go.dev/github.com/goptics/varmq#Topic.Publish) |
| You get back | a job handle and a `bool` | a `bool` |

**Topics are in-memory only.** A topic lives in your process. Nothing is written to disk, and a payload is lost when the process exits.

## Creating a topic and subscribing

[`NewTopic`](https://pkg.go.dev/github.com/goptics/varmq#NewTopic) creates a topic for payloads of one type. [`WithTopic`](https://pkg.go.dev/github.com/goptics/varmq#IWorkerBinder.WithTopic) subscribes a worker to it:

```go
package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	topic := varmq.NewTopic[int]()

	email := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(500 * time.Millisecond)
		fmt.Println("email for order", j.Data())
	})

	stock := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(10 * time.Millisecond)
		fmt.Println("stock for order", j.Data())
	}, 2)

	email.WithTopic(topic)
	stock.WithTopic(topic)

	for i := range 4 {
		topic.Publish(i) // both workers get their own job for this order
	}

	email.WaitUntilIdle()
	stock.WaitUntilIdle()
}
```

Each worker prints all four orders. The stock lines come first because its jobs are shorter, and with concurrency 2 two of them can print in either order.

![Two workers subscribed to one topic. Each Publish stores the payload once in a shared log and wakes both workers. Each worker reads it through its own cursor and runs its own job](https://raw.githubusercontent.com/goptics/varmq/main/assets/diagrams/varmq-topic.svg)

`WithTopic` is available on the worker that [`NewWorker`](https://pkg.go.dev/github.com/goptics/varmq#NewWorker) returns. Error and result workers cannot subscribe: see the table in [worker types](https://varmq.goptics.org/guides/worker-types/#which-queues-each-worker-can-bind).

`WithTopic` returns a [`Subscription`](https://pkg.go.dev/github.com/goptics/varmq#Subscription), the worker's handle on the topic. A subscription has no `Add`, because the topic is the only producer. Two calls panic: subscribing a worker to a topic it is already subscribed to, and passing a `nil` topic. A worker can subscribe again after it unsubscribes.

## Publishing

`Publish` stores the payload once and wakes every subscribed worker. Each worker then makes its own job from the payload. The payload is copied by value, so if `T` is a pointer or holds pointers, maps or slices, every subscriber shares what they point to.

`Publish` returns `true` once the payload is stored. It returns `false`, and no subscriber gets the payload, in three cases:

- no worker is subscribed, so the payload has nowhere to go;
- the topic is closed;
- the topic is full: see [capacity](#capacity).

It returns when the payload is stored, not when the jobs finish. To wait for a subscriber's jobs, wait on that subscriber's worker: [waiting](https://varmq.goptics.org/guides/waiting/) covers how. A job made from a payload gets its ID from the subscriber's own [job ID generator](https://varmq.goptics.org/guides/job-ids/), and a worker with no generator gives it none.

A worker receives only the payloads published after it subscribed. A payload that `Publish` refused for lack of subscribers is not kept for the next one.

```go
package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	topic := varmq.NewTopic[int]()

	// Nobody is subscribed yet, so the payload has nowhere to go.
	fmt.Println("publish 1:", topic.Publish(1))

	email := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("email got", j.Data())
	})
	email.WithTopic(topic)
	topic.Publish(2)
	email.WaitUntilIdle()

	audit := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(10 * time.Millisecond)
		fmt.Println("audit got", j.Data())
	})
	audit.WithTopic(topic)
	topic.Publish(3)

	email.WaitUntilIdle()
	audit.WaitUntilIdle()
}
```

`Publish` refuses payload 1 because nobody is subscribed, and that payload is gone. The `email` worker subscribes before payload 2 and gets 2 and 3. The `audit` worker subscribes after payload 2, so it gets only 3.

## How a topic stores payloads

A topic keeps one copy of each payload in a shared in-memory log, however many workers subscribe. Each subscription reads the log at its own position, so a slow worker does not block `Publish` or the other workers. A payload is freed once every subscription has read it. In the diagram, that is why a chunk of the log stays while the slower worker's cursor is still on it.

So a subscriber that is slow, paused or stopped keeps every payload that it has not read. It keeps them until it reads them all, purges or unsubscribes. For a worker that may stall, set a [capacity](#capacity).

## Unsubscribing

[`Unsubscribe`](https://pkg.go.dev/github.com/goptics/varmq#Subscription.Unsubscribe) on the subscription ends it. The worker gets nothing more from the topic, and the payloads it has not read yet are dropped. A job the worker has already taken from the topic is not recalled. A running job finishes, and a job waiting in a batch still runs. Calling it twice is safe. [`Close`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue.Close) on the subscription does the same.

[`Purge`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue.Purge) on a subscription keeps the subscription and drops the unread payloads. Unlike `Unsubscribe`, it also discards the jobs the worker has taken from the topic but not started. So a job waiting in a batch does not run. A job that is already running finishes. `Purge` does not call `Close` on the payloads. [`Len`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue.Len) is the number of payloads the subscription has not read, and [`Worker()`](https://pkg.go.dev/github.com/goptics/varmq#Subscription.Worker) returns its worker.

To end every subscription at once, call [`Close`](https://pkg.go.dev/github.com/goptics/varmq#Topic.Close) on the topic. It unsubscribes all of them, and every later `Publish` returns `false`. [`NumSubscribers()`](https://pkg.go.dev/github.com/goptics/varmq#Topic.NumSubscribers) tells you how many are attached.

```go
package main

import (
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	topic := varmq.NewTopic[int]()

	email := varmq.NewWorker(func(j varmq.Job[int]) {
		fmt.Println("email got", j.Data())
	})

	stock := varmq.NewWorker(func(j varmq.Job[int]) {
		time.Sleep(10 * time.Millisecond)
		fmt.Println("stock got", j.Data())
	})

	email.WithTopic(topic)
	stockSub := stock.WithTopic(topic)

	topic.Publish(1)
	email.WaitUntilIdle()
	stock.WaitUntilIdle()

	stockSub.Unsubscribe()
	fmt.Println("subscribers:", topic.NumSubscribers())

	topic.Publish(2)
	email.WaitUntilIdle()
	stock.WaitUntilIdle()
}
```

Both workers get payload 1. After the stock subscription ends, one subscriber is left, and payload 2 goes only to `email`.

## Capacity

A topic has no limit unless you set one. [`WithTopicCapacity`](https://pkg.go.dev/github.com/goptics/varmq#WithTopicCapacity) bounds it when you create it.

The limit applies to the slowest subscription. Once it has that many payloads unread, `Publish` returns `false`, the way `Add` does on a full queue, and the refused payload reaches no subscriber. Room comes back as that subscription reads, purges or unsubscribes. A value below 1 means no limit.

```go
package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	topic := varmq.NewTopic[int](varmq.WithTopicCapacity(2))

	// Not started, so it reads nothing and payloads pile up unread.
	worker := varmq.NewWorker(
		func(j varmq.Job[int]) {
			fmt.Println("got", j.Data())
		},
		varmq.WithAutoRun(false),
	)
	worker.WithTopic(topic)

	fmt.Println("publish 1:", topic.Publish(1))
	fmt.Println("publish 2:", topic.Publish(2))
	fmt.Println("publish 3:", topic.Publish(3), "- IsFull:", topic.IsFull())

	worker.Start()
	worker.WaitUntilIdle()

	ok := topic.Publish(3)
	worker.WaitUntilIdle()
	fmt.Println("publish 3 again:", ok)
}
```

The worker is created with [`WithAutoRun(false)`](https://pkg.go.dev/github.com/goptics/varmq#WithAutoRun), so it reads nothing until [`Start`](https://pkg.go.dev/github.com/goptics/varmq#Worker.Start). Two payloads fit, and the third is refused while 2 are unread. Once the worker has read both, there is room, and the same `Publish` succeeds.

[`IsFull()`](https://pkg.go.dev/github.com/goptics/varmq#Topic.IsFull) on the topic tells you whether it is at its capacity right now, and the answer can change right after. On a subscription, [`IsFull()`](https://pkg.go.dev/github.com/goptics/varmq#IExternalBaseQueue.IsFull) is true when that subscription is at the capacity, which makes it the one refusing `Publish`. Act on the result of `Publish` itself, not on `IsFull`.

## Priority and strategy

A topic subscription is one more source of its worker, next to any queues bound to it. The worker's [strategy](https://varmq.goptics.org/guides/strategies/) picks which source gives the next job. [`WithTopicPriority`](https://pkg.go.dev/github.com/goptics/varmq#WithTopicPriority) ranks the subscription the way [`WithQueuePriority`](https://pkg.go.dev/github.com/goptics/varmq#WithQueuePriority) ranks a queue: a lower number goes first, and without it the subscription ranks last.

```go
worker.WithTopic(orders, varmq.WithTopicPriority(1)) // read orders before other sources
```

With the default [`Priority`](https://pkg.go.dev/github.com/goptics/varmq#Priority) strategy, a worker with a topic subscription ranked 1 and a queue without a rank takes the payloads first. If the two have the same rank, the one bound first goes first. [Strategies](https://varmq.goptics.org/guides/strategies/#what-priority-does-under-the-other-strategies) covers what priority does under the other three.
