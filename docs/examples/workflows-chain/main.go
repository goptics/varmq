package main

import (
	"context"
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	// 1. The workflow, on a store that keeps its state.
	wf := varmq.NewWorkflow(varmq.NewWorkflowStore())

	// 2. A worker and its queue. Here one worker runs every task.
	steps := varmq.NewErrWorker(func(j varmq.Job[string]) error {
		fmt.Println("run:", j.Data())
		return nil
	}).BindQueue()

	// 3-5. The tasks, one Add each. After sets the order.
	download := wf.Add("download", steps, "download photo.png")
	resize := wf.Add("resize", steps, "resize to 64px").After(download)
	wf.Add("save", steps, "save to disk").After(resize)

	// 6. Submit, then wait for the end.
	if err := wf.Submit(context.Background()); err != nil {
		panic(err)
	}
	rec, err := wf.Wait()
	if err != nil {
		panic(err)
	}
	fmt.Println("workflow:", rec.State)
}
