package main

import (
	"fmt"
	"time"

	"github.com/goptics/sqliteq"
	"github.com/goptics/varmq"
	"github.com/lucsky/cuid"
)

func main() {
	w := varmq.NewVoidWorker(func(num int) {
		println(num)
		time.Sleep(1 * time.Second)
	}, 2)
	sq := sqliteq.New("test.db")
	pq, err := sq.NewQueue("test", sqliteq.WithRemoveOnComplete(false))

	if err != nil {
		fmt.Println(err)
		return
	}

	q := w.WithPersistentQueue(pq)
	defer q.WaitUntilFinished()

	for i := range 20 {
		q.Add(i, varmq.WithJobId(cuid.New()))
	}

	fmt.Println("done")
}
