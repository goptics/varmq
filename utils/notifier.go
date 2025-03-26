package utils

type Notifier chan struct{}

func NewNotifier(bufferSize uint32) Notifier {
	return make(Notifier, bufferSize)
}

func (n Notifier) Send() {
	select {
	case n <- struct{}{}:
	default:
		// channel is full, so ignoring
	}
}

func (n Notifier) Receive(fn func()) {
	for range n {
		fn()
	}
}

func (n Notifier) Close() error {
	close(n)
	return nil
}
