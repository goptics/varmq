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
