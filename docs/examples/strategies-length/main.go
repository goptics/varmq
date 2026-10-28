package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func run(name string, strategy varmq.Strategy) {
	worker := varmq.NewWorker(
		func(j varmq.Job[string]) {
			fmt.Print(" ", j.Data())
		},
		varmq.WithStrategy(strategy),
		varmq.WithAutoRun(false),
	)

	alerts := worker.BindQueue()
	reports := worker.BindQueue()
	emails := worker.BindQueue()

	alerts.Add("alert")
	alerts.Add("alert")
	reports.Add("report")
	emails.Add("email")
	emails.Add("email")
	emails.Add("email")

	fmt.Print(name, ":")
	worker.Start()
	worker.WaitUntilIdle()
	fmt.Println()
}

func main() {
	run("MaxLen", varmq.MaxLen)
	run("MinLen", varmq.MinLen)
}
