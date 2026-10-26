package main

import (
	"fmt"
	"slices"
	"sync"

	"github.com/goptics/varmq"
)

// inbox records the payloads one worker received.
type inbox struct {
	mu   sync.Mutex
	got  []int
	name string
}

func (b *inbox) add(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.got = append(b.got, n)
}

// show prints the payloads in sorted order, because a worker with
// concurrency above 1 may finish them in any order.
func (b *inbox) show() {
	b.mu.Lock()
	defer b.mu.Unlock()
	slices.Sort(b.got)
	fmt.Printf("%s received %v\n", b.name, b.got)
}

func main() {
	topic := varmq.NewTopic[int]()

	// No worker is subscribed yet, so Publish refuses the payload.
	fmt.Println("publish with no subscribers:", topic.Publish(0))

	email, stock, audit := &inbox{name: "email"}, &inbox{name: "stock"}, &inbox{name: "audit"}

	emailWorker := varmq.NewWorker(func(j varmq.Job[int]) { email.add(j.Data()) }, 1)
	stockWorker := varmq.NewWorker(func(j varmq.Job[int]) { stock.add(j.Data()) }, 2)
	emailWorker.WithTopic(topic)
	stockSub := stockWorker.WithTopic(topic)

	// Both subscribed workers get their own job for each payload.
	topic.Publish(1)
	topic.Publish(2)

	// Publish returns once the payload is stored. To wait for the jobs, wait on each worker.
	emailWorker.WaitUntilIdle()
	stockWorker.WaitUntilIdle()

	// A worker that subscribes later receives only what is published after that.
	auditWorker := varmq.NewWorker(func(j varmq.Job[int]) { audit.add(j.Data()) }, 1)
	auditWorker.WithTopic(topic)
	fmt.Println("subscribers:", topic.NumSubscribers())
	topic.Publish(3)
	emailWorker.WaitUntilIdle()
	stockWorker.WaitUntilIdle()
	auditWorker.WaitUntilIdle()

	// After Unsubscribe the stock worker gets nothing more.
	stockSub.Unsubscribe()
	fmt.Println("after Unsubscribe, subscribers:", topic.NumSubscribers())
	topic.Publish(4)
	emailWorker.WaitUntilIdle()
	auditWorker.WaitUntilIdle()

	email.show()
	stock.show()
	audit.show()

	// A topic with a capacity refuses a payload once its slowest subscriber has
	// that many unread. This worker is not started, so it reads nothing yet.
	small := varmq.NewTopic[int](varmq.WithTopicCapacity(2))
	slow := &inbox{name: "slow"}
	slowWorker := varmq.NewWorker(func(j varmq.Job[int]) { slow.add(j.Data()) }, 1, varmq.WithAutoRun(false))
	slowWorker.WithTopic(small)

	fmt.Println("publish 1:", small.Publish(1))
	fmt.Println("publish 2:", small.Publish(2))
	fmt.Println("publish 3:", small.Publish(3), "- IsFull:", small.IsFull())

	slowWorker.Start()
	slowWorker.WaitUntilIdle()
	fmt.Println("publish 3 after the worker caught up:", small.Publish(3))
	slowWorker.WaitUntilIdle()
	slow.show()
}
