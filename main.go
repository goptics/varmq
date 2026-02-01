package varmq

import (
	"errors"

	"github.com/goptics/varmq/utils"
)

// NewWorker creates a worker that can be bound to standard, priority, and persistent and distributed queue types.
// If concurrency in config is not provided, it defaults to 1.
// and if concurrency is less than 1, it defaults to number of CPU cores.
// Parameters:
//   - wf: Worker function that processes queue items
//   - config: Optional configuration parameters (concurrency, idle worker ratio, etc.)
//
// Example:
//
//	worker := NewWorker(func(data string) {
//	    return len(data), nil
//	}, 4) // 4 concurrent workers
//	queue := worker.BindQueue() // Bind to standard queue
//	priorityQueue := worker.BindPriorityQueue() // Bind to priority queue
//	persistentQueue := worker.BindPersistentQueue(myPersistentQueue) // Bind to persistent queue
//	distQueue := worker.BindDistributedQueue(myDistributedQueue) // Bind to distributed queue
func NewWorker[T any](wf func(j Job[T]), config ...any) IWorkerBinder[T] {
	var w *worker[T, iJob[T]]
	w = newWorker(func(ij iJob[T]) {
		// TODO: The panic error will be passed through inside logger in future
		panicErr := utils.WithSafe("worker", func() {
			wf(ij)
		})

		// Track failed if panic occurred
		if panicErr != nil {
			w.metrics.incFailed()
		} else {
			w.metrics.incSuccessful()
		}
	}, config...)

	return newQueues(w)
}

// NewErrWorker creates a worker for operations that only return errors (no result value).
// This is useful for operations where you only care about success/failure status.
// If concurrency in config is not provided, it defaults to 1.
// and if concurrency is less than 1, it defaults to number of CPU cores.
// Unlike NewWorker, it can't be bound to distributed or persistent queue.
// Parameters:
//   - wf: Worker function that processes items and returns only error
//   - config: Optional configuration parameters (concurrency, idle worker ratio, etc.)
//
// Example:
//
//	worker := NewErrWorker(func(data int) error {
//	    log.Printf("Processing: %d", data)
//	    return nil
//	}, 4) // 4 concurrent workers
//	queue := worker.BindQueue() // Bind to standard queue
//	priorityQueue := worker.BindPriorityQueue() // Bind to priority queue
func NewErrWorker[T any](wf func(j Job[T]) error, config ...any) IErrWorkerBinder[T] {
	var w *worker[T, iErrorJob[T]]
	w = newErrWorker(func(ij iErrorJob[T]) {
		var panicErr error
		var err error

		panicErr = utils.WithSafe("err-worker", func() {
			err = wf(ij)
		})

		// send error if any
		if err := utils.SelectError(panicErr, err); err != nil {
			ij.sendError(err)
			w.metrics.incFailed()
		} else {
			w.metrics.incSuccessful()
		}
	}, config...)

	return newErrQueues(w)
}

// NewResultWorker creates a worker for operations that return a result value and error.
// If concurrency in config is not provided, it defaults to 1.
// and if concurrency is less than 1, it defaults to number of CPU cores.
// Unlike NewWorker, it can't be bound to distributed or persistent queue.
// Parameters:
//   - wf: Worker function that processes items and returns a result and error
//   - config: Optional configuration parameters (concurrency, idle worker ratio, etc.)
//
// Example:
//
//	worker := NewResultWorker(func(data string) (int, error) {
//	    return len(data), nil
//	}, 4) // 4 concurrent workers
//	queue := worker.BindQueue() // Bind to standard queue
//	priorityQueue := worker.BindPriorityQueue() // Bind to priority queue
func NewResultWorker[T, R any](wf func(j Job[T]) (R, error), config ...any) IResultWorkerBinder[T, R] {
	var w *worker[T, iResultJob[T, R]]
	w = newResultWorker(func(ij iResultJob[T, R]) {
		var panicErr error
		var err error

		panicErr = utils.WithSafe("result-worker", func() {
			result, e := wf(ij)
			if e != nil {
				err = e
			} else {
				ij.sendResult(result)
			}
		})

		// send error if any
		if err := utils.SelectError(panicErr, err); err != nil {
			ij.sendError(err)
			w.metrics.incFailed()
		} else {
			w.metrics.incSuccessful()
		}
	}, config...)

	return newResultQueues(w)
}

var errNilFunction = errors.New("provided function is nil")

// Func is a helper function that enables direct function submission to VarMQ workers.
// Instead of creating custom data types and worker functions, you can pass functions directly.
//
// Usage:
//
//	queue := varmq.NewWorker(varmq.Func()).BindQueue()
//	queue.Add(func() {
//	    fmt.Println("Hello from worker!")
//	})
//
// Note: Func does not support persistence or distribution since functions are not
// serializable. For persistent or distributed job queues, use custom or primitive data types that can
// be serialized to JSON instead of passing functions directly.
func Func() func(j Job[func()]) {
	return func(j Job[func()]) {
		if fn := j.Data(); fn != nil {
			fn()
			return
		}

		panic(errNilFunction)
	}
}

// ErrFunc is a helper function for functions that return errors.
// It enables direct submission of functions that return error values.
//
// Usage:
//
//	queue := varmq.NewErrWorker(varmq.ErrFunc()).BindQueue()
//	job, ok := queue.Add(func() error {
//	    if err := doSomething(); err != nil {
//	        return fmt.Errorf("failed to do something: %w", err)
//	    }
//	    return nil
//	})
//
//	if err := job.Err(); err != nil {
//	    log.Printf("Job failed: %v", err)
//	}
func ErrFunc() func(j Job[func() error]) error {
	return func(j Job[func() error]) error {
		if fn := j.Data(); fn != nil {
			return fn()
		}

		return errNilFunction
	}
}

// ResultFunc is a generic helper function for functions that return both a result and an error.
// It enables direct submission of functions with return values of any type.
//
// Usage:
//
//	queue := varmq.NewResultWorker(varmq.ResultFunc[string]()).BindQueue()
//	job, ok := queue.Add(func() (string, error) {
//	    result, err := fetchData()
//	    if err != nil {
//	        return "", fmt.Errorf("failed to fetch: %w", err)
//	    }
//	    return result, nil
//	})
//
//	result, err := job.Result()
//	if err != nil {
//	    log.Printf("Job failed: %v", err)
//	} else {
//	    fmt.Printf("Result: %s", result)
//	}
//
// The generic type parameter R specifies the return type of your function.
func ResultFunc[R any]() func(j Job[func() (R, error)]) (R, error) {
	return func(j Job[func() (R, error)]) (R, error) {
		if fn := j.Data(); fn != nil {
			return fn()
		}

		return *new(R), errNilFunction
	}
}
