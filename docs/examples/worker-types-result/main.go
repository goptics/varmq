package main

import (
	"fmt"
	"strconv"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewResultWorker(func(j varmq.Job[string]) (int, error) {
		return strconv.Atoi(j.Data())
	}, 1)
	queue := worker.BindQueue()

	for _, s := range []string{"42", "forty-two"} {
		job, _ := queue.Add(s)
		n, err := job.Result()
		fmt.Printf("%q: %d, %v\n", s, n, err)
	}
}
