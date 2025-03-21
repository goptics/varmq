package gocq

import (
	"sync"
)

type syncGroup struct {
	wg sync.WaitGroup
	mx sync.Mutex
}
