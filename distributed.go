package varmq

// DistributedQueue is a external queue wrapper of an any IDistributedQueue
type DistributedQueue[T any] interface {
	TaskQueue[T] // a workflow task can target it
	IExternalBaseQueue
	// Add data to the queue
	Add(data T, configs ...JobConfigFunc) bool
}

type distributedQueue[T any] struct {
	IDistributedQueue
	config queueConfig
}

// Deprecated: use NewQueue, which takes any shared storage and returns a
// job handle.
func NewDistributedQueue[T any](internalQueue IDistributedQueue, configs ...QueueConfigFunc) DistributedQueue[T] {
	c := loadQueueConfigs(configs...)

	if cap, ok := internalQueue.(CapacitySetter); ok {
		cap.SetCapacity(c.capacity)
	}

	return &distributedQueue[T]{
		IDistributedQueue: internalQueue,
		config:            c,
	}
}

func (dq *distributedQueue[T]) Pause() error   { return pauseQueue(dq.IDistributedQueue) }
func (dq *distributedQueue[T]) Resume() error  { return resumeQueue(dq.IDistributedQueue) }
func (dq *distributedQueue[T]) IsPaused() bool { return isQueuePaused(dq.IDistributedQueue) }

func (dq *distributedQueue[T]) NumPending() int {
	return dq.Len()
}

func (dq *distributedQueue[T]) IsFull() bool {
	return dq.config.capacity > 0 && dq.Len() >= dq.config.capacity
}

func (dq *distributedQueue[T]) Add(data T, c ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(newConfig(), c...))

	jBytes, err := j.Json()

	if err != nil {
		j.Close()
		return false
	}

	if ok := enqueue(dq.IDistributedQueue, jBytes, j.id); !ok {
		j.Close()
		return false
	}

	return true
}

// WorkflowItem makes the handle a workflow task target (workflow_target.go).
func (dq *distributedQueue[T]) WorkflowItem(data T) (WorkflowItem, error) {
	return durableWorkflowItem(dq.IDistributedQueue, false, data)
}
