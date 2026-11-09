package main

import (
	"context"
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	// Each step has its own worker, queue and data type.
	upload := varmq.NewResultWorker(func(j varmq.Job[string]) (string, error) {
		return "s3://bucket/" + j.Data(), nil
	}).BindQueue()

	// A result worker's return value is the task's result.
	thumb := varmq.NewResultWorker(func(j varmq.Job[int]) (string, error) {
		var src string
		if err := varmq.TaskOf(j).Result("upload", &src); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s@%dpx", src, j.Data()), nil
	}, 2).BindQueue()

	email := varmq.NewErrWorker(func(j varmq.Job[string]) error {
		t := varmq.TaskOf(j)
		for _, dep := range []string{"thumb-s", "thumb-l"} {
			var url string
			if err := t.Result(dep, &url); err != nil {
				return err
			}
			fmt.Println("email to", j.Data(), "with", url)
		}
		return nil
	}).BindQueue()

	wf := varmq.NewWorkflow(varmq.NewWorkflowStore(), varmq.WithWorkflowID("upload-1"))
	up := wf.Add("upload", upload, "photo.png")
	small := wf.Add("thumb-s", thumb, 64).After(up)
	large := wf.Add("thumb-l", thumb, 512).After(up)
	wf.Add("email", email, "ann@example.com").After(small, large)

	if err := wf.Submit(context.Background()); err != nil {
		panic(err)
	}

	// Wait with a deadline, so a stuck workflow cannot block forever.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rec, err := wf.WaitContext(ctx)
	if err != nil {
		panic(err)
	}

	fmt.Println("workflow:", rec.State)
	for _, t := range rec.Tasks {
		fmt.Printf("  %-8s %-10s result: %d bytes\n", t.Name, t.State, t.ResultSize)
	}
}
