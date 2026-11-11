// The workflow of the home page's map (src/lib/heromap/chapters.ts, chapter 9, and src/lib/story/stations.ts):
// four tasks on one result worker's queue. charge fails, ship is cancelled, and the undo job of reserve runs.
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/goptics/varmq"
)

func main() {
	// One worker with one goroutine runs every task, and the undo job too.
	queue := varmq.NewResultWorker(func(j varmq.Job[string]) (string, error) {
		if j.Data() == "charge card" {
			fmt.Println("run: charge card - declined")
			return "", errors.New("card declined")
		}
		fmt.Println("run:", j.Data())
		return "ok", nil
	}).BindQueue()

	wf := varmq.NewWorkflow(varmq.NewWorkflowStore())

	r := wf.Add("reserve", queue, "reserve stock")
	r.Undo(queue, "release stock")
	l := wf.Add("label", queue, "print label").After(r)
	c := wf.Add("charge", queue, "charge card").After(r)
	wf.Add("ship", queue, "ship order").After(l, c)

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
		if t.Undo != nil {
			line += fmt.Sprintf(" (undo: %s)", t.Undo.State)
		}
		fmt.Println(line)
	}
}
