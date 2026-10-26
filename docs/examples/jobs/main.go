package main

import (
	"errors"
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	started := make(chan struct{})
	release := make(chan struct{})

	// One worker, one job at a time. The function sees a Job[string]: its
	// Data and its ID, nothing else.
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Printf("running %s (id %s)\n", j.Data(), j.ID())
		if j.ID() == "job-1" {
			close(started)
			<-release // job-1 stays here until main closes release
		}
	}, 1)
	queue := worker.BindQueue()

	// Add returns a handle to the job: an EnqueuedJob. WithJobId names the job.
	first, _ := queue.Add("resize photo", varmq.WithJobId("job-1"))
	<-started // job-1 is running now, and the worker has no free slot

	second, _ := queue.Add("send email", varmq.WithJobId("job-2"))
	third, _ := queue.Add("build report", varmq.WithJobId("job-3"))

	handles := []varmq.EnqueuedJob{first, second, third}
	for _, h := range handles {
		fmt.Println(h.ID(), h.Status())
	}

	// A running job cannot be closed.
	err := first.Close()
	fmt.Println("close job-1:", err)
	fmt.Println("is ErrJobProcessing:", errors.Is(err, varmq.ErrJobProcessing))

	// A queued job can. Close releases Wait at once, and the worker skips it.
	fmt.Println("close job-2:", second.Close())
	second.Wait()
	fmt.Println("job-2 wait returned, status", second.Status())

	// Closing it again is an error.
	fmt.Println("close job-2 again:", second.Close())

	// Let job-1 finish. The worker then runs job-3 and never job-2.
	close(release)
	for _, h := range handles {
		h.Wait()
	}

	for _, h := range handles {
		fmt.Println(h.ID(), h.Status())
	}
}
