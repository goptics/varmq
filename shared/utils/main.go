package utils

import (
	"fmt"
	"runtime"
)

// Cpus returns the number of logical CPUs available on the system.
func Cpus() uint32 {
	return uint32(runtime.NumCPU())
}

// Safe runs the provided function and returns any panic errors.
func Safe(name string, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered inside %s: %v", name, r)
		}
	}()

	fn()

	return err
}

// SafeGo runs the provided function in a goroutine and returns a channel that will receive any panic errors.
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
