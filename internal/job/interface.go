package job

import "io"

type IJob interface {
	IsClosed() bool
	State() string
	io.Closer
}

type AwaitableJob[T any] interface {
	IJob
	WaitForResult() (T, error)
}

type AwaitableVoidJob[T any] interface {
	IJob
	WaitForError() error
}
