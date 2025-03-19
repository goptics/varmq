package common

import (
	"fmt"
	"runtime"
)

const AddAllSampleSize = 100

// Double multiplies the input by 2.
func Double(n int) int {
	return n * 2
}

// Cpus returns the number of logical CPUs available on the system.
func Cpus() uint32 {
	return uint32(runtime.NumCPU())
}

func Safe(name string, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered inside %s: %v", name, r)
		}
	}()

	fn()

	return err
}

func SafeGo(name string, fn func()) <-chan error {
	err := make(chan error, 1)
	go func() {
		if e := Safe(name, fn); e != nil {
			err <- e
		}
		close(err)
	}()

	return err
}
