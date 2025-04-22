package utils

import (
	"fmt"
	"runtime"
)

// Cpus returns the number of logical CPUs available on the system.
func Cpus() uint32 {
	return uint32(runtime.NumCPU())
}

// WithSafe runs the provided function and returns any panic errors as an error.
func WithSafe(name string, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered inside %s: %v", name, r)
		}
	}()

	fn()

	return err
}

// WithSafeGo runs the provided function in a goroutine and returns a channel that will receive any panic errors as an error.
func GoWithSafe(name string, fn func()) <-chan error {
	err := make(chan error, 1)
	go func() {
		if e := WithSafe(name, fn); e != nil {
			err <- e
		}
		close(err)
	}()

	return err
}
