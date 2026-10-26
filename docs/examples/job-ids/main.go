package main

import (
	"fmt"
	"sync/atomic"

	"github.com/goptics/varmq"
)

func main() {
	var next atomic.Int32

	// The worker is not started, so jobs wait in the queue. The generator
	// gives an ID to a job added without one.
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Printf("running %s with id %q\n", j.Data(), j.ID())
	}, 1,
		varmq.WithAutoRun(false),
		varmq.WithJobIdGenerator(func() string {
			return fmt.Sprintf("auto-%d", next.Add(1))
		}),
	)
	queue := worker.BindQueue()

	// No WithJobId: the generator names the job.
	report, _ := queue.Add("report")
	fmt.Println("generated:", report.ID())

	// WithJobId names a job. While it waits in the queue, a second Add with
	// the same ID is refused and returns a nil handle.
	first, ok := queue.Add("refresh 1", varmq.WithJobId("refresh"))
	fmt.Println("first refresh:", ok, first.ID())
	second, ok := queue.Add("refresh 2", varmq.WithJobId("refresh"))
	fmt.Println("second refresh:", ok, "- handle is nil:", second == nil)

	// Item.ID names a job added with AddAll. The ID gets a "g:" prefix, so
	// this item does not collide with the "refresh" above.
	group := queue.AddAll([]varmq.Item[string]{{ID: "refresh", Data: "refresh 3"}})
	fmt.Println("group queued:", group.NumPending(), "- Len:", queue.Len())

	worker.Start()
	worker.WaitUntilIdle()

	// The jobs have left the queue, so the ID is free again.
	again, ok := queue.Add("refresh 4", varmq.WithJobId("refresh"))
	again.Wait()
	fmt.Println("refresh after it ran:", ok)
}
