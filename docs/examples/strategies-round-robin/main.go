package main

import (
	"fmt"

	"github.com/goptics/varmq"
)

func main() {
	worker := varmq.NewWorker(func(j varmq.Job[string]) {
		fmt.Println(j.Data())
	}, 1, varmq.WithStrategy(varmq.RoundRobin), varmq.WithAutoRun(false))

	alerts := worker.BindQueue()
	reports := worker.BindQueue()
	emails := worker.BindQueue()

	alerts.Add("alert 1")
	alerts.Add("alert 2")
	reports.Add("report 1")
	emails.Add("email 1")
	emails.Add("email 2")
	emails.Add("email 3")

	worker.Start()
	worker.WaitUntilIdle()
}
