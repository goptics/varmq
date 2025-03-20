package job

type Notifier chan struct{}

func NewNotifier(bufferSize uint32) Notifier {
	return make(Notifier, bufferSize)
}

func (j Notifier) Notify() {
	select {
	case j <- struct{}{}:
	default:
		// channel is full, so ignoring
	}
}

func (j Notifier) Listen(fn func()) {
	for range j {
		fn()
	}
}

func (j Notifier) Close() error {
	close(j)
	return nil
}
