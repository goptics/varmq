package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	steps := varmq.NewErrWorker(func(j varmq.Job[string]) error {
		if j.Data() == "charge card" {
			fmt.Println("run: charge card - declined")
			return errors.New("card declined")
		}
		fmt.Println("run:", j.Data())
		return nil
	}).BindQueue()

	undos := varmq.NewErrWorker(func(j varmq.Job[string]) error {
		fmt.Println("undo:", j.Data(), "- for task", varmq.TaskOf(j).Name)
		return nil
	}).BindQueue()

	wf := varmq.NewWorkflow(varmq.NewWorkflowStore(), varmq.WithWorkflowID("order-1"))
	reserve := wf.Add("reserve", steps, "reserve stock").Undo(undos, "release stock")
	label := wf.Add("label", steps, "print label").After(reserve) // no undo
	wf.Add("charge", steps, "charge card").After(label)

	if err := wf.Submit(context.Background()); err != nil {
		panic(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rec, err := wf.WaitContext(ctx)
	if err != nil {
		panic(err)
	}

	fmt.Println("workflow:", rec.State)
	for _, t := range rec.Tasks {
		line := fmt.Sprintf("  %-8s %s", t.Name, t.State)
		if t.Reason != "" {
			line += " - " + t.Reason
		}
		if t.Undo != nil {
			line += fmt.Sprintf(" (undo: %s)", t.Undo.State)
		}
		fmt.Println(line)
	}
}
