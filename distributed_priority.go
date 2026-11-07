package varmq

// DistributedPriorityQueue is a external queue wrapper of an any IDistributedPriorityQueue
type DistributedPriorityQueue[T any] interface {
	IExternalBaseQueue

	// Add data to the queue with priority
	Add(data T, priority int, configs ...JobConfigFunc) bool
}

type distributedPriorityQueue[T any] struct {
	IDistributedPriorityQueue
	config queueConfig
}

func NewDistributedPriorityQueue[T any](internalQueue IDistributedPriorityQueue, configs ...QueueConfigFunc) DistributedPriorityQueue[T] {
	cfg := loadQueueConfigs(configs...)

	if cap, ok := internalQueue.(CapacitySetter); ok {
		cap.SetCapacity(cfg.capacity)
	}

	return &distributedPriorityQueue[T]{
		IDistributedPriorityQueue: internalQueue,
		config:                    cfg,
	}
}

func (dpq *distributedPriorityQueue[T]) Pause() error {
	return pauseQueue(dpq.IDistributedPriorityQueue)
}
func (dpq *distributedPriorityQueue[T]) Resume() error {
	return resumeQueue(dpq.IDistributedPriorityQueue)
}
func (dpq *distributedPriorityQueue[T]) IsPaused() bool {
	return isQueuePaused(dpq.IDistributedPriorityQueue)
}

func (dpq *distributedPriorityQueue[T]) NumPending() int {
	return dpq.Len()
}

func (dpq *distributedPriorityQueue[T]) IsFull() bool {
	return dpq.config.capacity > 0 && dpq.Len() >= dpq.config.capacity
}

func (dpq *distributedPriorityQueue[T]) Add(data T, priority int, c ...JobConfigFunc) bool {
	j := newJob(data, loadJobConfigs(newConfig(), c...))

	jBytes, err := j.Json()

	if err != nil {
		j.Close()
		return false
	}

	if ok := enqueuePriority(dpq.IDistributedPriorityQueue, jBytes, priority, j.id); !ok {
		j.Close()
		return false
	}

	return true
}

// taskTarget and taskItem make the handle a workflow task target (workflow_target.go).
func (dpq *distributedPriorityQueue[T]) taskTarget() taskTarget {
	return durableTaskTarget(dpq.IDistributedPriorityQueue, true)
}
func (dpq *distributedPriorityQueue[T]) taskItem(data T, jobID string, tag taskTag) (any, error) {
	return durableTaskItem(data, jobID, tag)
}
