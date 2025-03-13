package shared

import "runtime"

const SampleSize = 100

// Double multiplies the input by 2.
func Double(n int) int {
	return n * 2
}

func Cpus() uint32 {
	return uint32(runtime.NumCPU())
}
