package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/goptics/varmq"
)

func main() {
	// A result worker that parses numbers. It is not started yet, so we can
	// look at the group before any job runs.
	worker := varmq.NewResultWorker(func(j varmq.Job[string]) (int, error) {
		return strconv.Atoi(j.Data())
	}, 3, varmq.WithAutoRun(false))
	queue := worker.BindQueue()

	items := []varmq.Item[string]{
		{ID: "a", Data: "10"},
		{ID: "b", Data: "x"},
		{ID: "c", Data: "30"},
		{ID: "a", Data: "99"}, // same ID as the first item: skipped
	}
	group := queue.AddAll(items)
	fmt.Println("queued:", group.NumPending(), "of", len(items))

	worker.Start()

	// Results closes once every job in the group is done. Results arrive in
	// the order the jobs finish, so collect them by JobId.
	byID := map[string]varmq.Result[int]{}
	for r := range group.Results() {
		byID[r.JobId] = r
	}
	for _, id := range slices.Sorted(maps.Keys(byID)) {
		r := byID[id]
		if r.Err != nil {
			fmt.Printf("%s failed: %v\n", id, r.Err)
			continue
		}
		fmt.Printf("%s = %d\n", id, r.Data)
	}
	fmt.Println("pending:", group.NumPending())

	// An error worker's group hands back only the errors.
	validator := varmq.NewErrWorker(func(j varmq.Job[int]) error {
		if j.Data() < 0 {
			return fmt.Errorf("%s: %d is negative", j.ID(), j.Data())
		}
		return nil
	}, 3)
	errGroup := validator.BindQueue().AddAll([]varmq.Item[int]{
		{ID: "p", Data: 5},
		{ID: "q", Data: -1},
		{ID: "r", Data: 7},
		{ID: "s", Data: -2},
	})

	var failures []string
	for err := range errGroup.Errs() {
		failures = append(failures, err.Error())
	}
	slices.Sort(failures)
	fmt.Println(len(failures), "of 4 failed")
	for _, f := range failures {
		fmt.Println(" ", f)
	}
}
