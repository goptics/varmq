package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// Job 2 returns an error, so one of the first three jobs counts as failed.
	worker := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		if j.Data() == 2 {
			return fmt.Errorf("job %d failed", j.Data())
		}
		return nil
	}, 1)
	queue := worker.BindQueue()
	m := worker.Metrics()

	show := func(label string) {
		fmt.Printf("%s: submitted %d, completed %d, successful %d, failed %d\n",
			label, m.Submitted(), m.Completed(), m.Successful(), m.Failed())
		fmt.Printf("  pending %d + processing %d + completed %d = %d\n",
			worker.NumPending(), worker.NumProcessing(), m.Completed(),
			worker.NumPending()+worker.NumProcessing()+int(m.Completed()))
	}

	for i := 1; i <= 3; i++ {
		queue.Add(i)
	}
	worker.WaitUntilIdle()
	show("after the run")

	// A paused worker takes no job, so three more jobs wait in the queue.
	worker.PauseAndWait()
	for i := 4; i <= 6; i++ {
		queue.Add(i)
	}
	show("paused, three jobs queued")

	// Purge closes the queued jobs. They stay in Submitted and never reach Completed.
	queue.Purge()
	show("after Purge")

	worker.Resume()
	worker.WaitUntilIdle()

	m.Reset()
	show("after Reset")
}
