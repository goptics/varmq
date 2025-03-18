package job

type Notifier chan struct{}

func NewNotifier() Notifier {
	return make(Notifier, 1)
}

func (j Notifier) Notify() {
	select {
	case j <- struct{}{}:
	default:
		// channel is full, so ignoring
	}
}

func (j Notifier) Close() error {
	close(j)
	return nil
}
