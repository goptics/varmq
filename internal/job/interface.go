package job

import "io"

type IJob interface {
	IsClosed() bool
	Status() string
	Drain()
	io.Closer
}
