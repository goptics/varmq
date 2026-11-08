package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	ctx := context.Background()

	// Each step has its own queue, worker and data type.
	upload := varmq.NewResultWorker(func(j varmq.Job[string]) (string, error) {
		return "s3://bucket/" + j.Data(), nil
	}).BindQueue()

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
				fmt.Println("email: no", dep, "-", err)
				continue
			}
			fmt.Println("email to", j.Data(), "with", url)
		}
		return nil
	}).BindQueue()

	unsend := varmq.NewErrWorker(func(j varmq.Job[string]) error {
		fmt.Println("undo email to", j.Data())
		return nil
	}).BindQueue()

	wf := varmq.NewWorkflow(varmq.NewWorkflowStore(), varmq.WithWorkflowID("upload-1"))
	up := wf.Add("upload", upload, "photo.png")
	small := wf.Add("thumb-s", thumb, 64).After(up)
	large := wf.Add("thumb-l", thumb, 512).After(up)
	wf.Add("email", email, "ann@example.com").
		After(small, large).
		Always().                       // send even if a thumbnail failed
		Undo(unsend, "ann@example.com") // queued only if email completed and the workflow fails or is cancelled

	if err := wf.Submit(ctx); err != nil {
		panic(err)
	}

	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rec, err := wf.WaitContext(wctx)
	if err != nil {
		panic(err)
	}

	out, _ := json.MarshalIndent(rec, "", "  ")
	fmt.Println(string(out))
}
