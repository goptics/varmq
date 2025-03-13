package job

import "io"

type IJob interface {
	IsClosed() bool
	State() string
	Drain()
	io.Closer
}
