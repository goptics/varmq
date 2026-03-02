package varmq

import (
	"context"
	"math"
	"time"

	"github.com/goptics/varmq/utils"
)

// ConfigFunc is a function that configures a worker.
type ConfigFunc func(*configs)

type configs struct {
	concurrency              uint32
	strategy                 Strategy
	minIdleWorkerRatio       uint8
	jobIdGenerator           func() string
	idleWorkerExpiryDuration time.Duration
	ctx                      context.Context
}

var defaultConfig = configs{
	concurrency: 1,
	jobIdGenerator: func() string {
		return ""
	},
	strategy: Priority,
}

// DefaultConcurrency sets the default concurrency level for all newly created workers.
// If concurrency is less than 1, it defaults to the number of CPU cores.
//
// Parameters:
//   - concurrency: The number of concurrent workers to use.
//     A value less than 1 will default to the number of CPU cores.
//
// Default: 1
func DefaultConcurrency(concurrency int) {
	defaultConfig.concurrency = withSafeConcurrency(concurrency)
}

// DefaultStrategy sets the default queue selection strategy for all newly created workers.
//
// Available strategies are:
//   - Priority: Selects the queue with the highest priority (default)
//   - RoundRobin: Selects queues in a round-robin fashion
//   - MaxLen: Selects the queue with the most items
//   - MinLen: Selects the queue with the fewest items
//
// Parameters:
//   - s: The strategy to use
//
// Default: Priority
func DefaultStrategy(s Strategy) {
	defaultConfig.strategy = s
}

// DefaultMinIdleWorkerRatio sets the default minimum percentage of idle workers to maintain
// in the pool as a proportion of the total concurrency level for all newly created workers.
//
// Maintaining some idle workers allows the system to respond quickly to incoming jobs
// without the overhead of creating new worker goroutines.
//
// Parameters:
//   - percentage: An integer between 1-100 representing the percentage of workers to keep idle.
//     Values outside the range 1-100 are automatically clamped (0 becomes 1, >100 becomes 100).
//
// Default: At least one idle worker is always maintained in the pool.
func DefaultMinIdleWorkerRatio(percentage uint8) {
	defaultConfig.minIdleWorkerRatio = clampPercentage(percentage)
}

// DefaultJobIdGenerator sets the default job ID generator function for all newly created workers.
// The provided function will be called to generate a unique ID for each job.
// If fn is nil, the call is a no-op.
//
// Parameters:
//   - fn: A function that returns a unique string ID.
//
// Default: A no-op generator that returns an empty string (no job ID).
func DefaultJobIdGenerator(fn func() string) {
	if fn == nil {
		return
	}
	defaultConfig.jobIdGenerator = fn
}

// DefaultIdleWorkerExpiryDuration sets the default time period after which idle workers are
// automatically removed from the worker pool for all newly created workers.
//
// This helps optimize resource usage by removing unnecessary idle workers when
// the system experiences prolonged periods of low activity.
//
// Parameters:
//   - duration: The time period a worker can remain idle before being removed
//     (e.g., 30*time.Second, 5*time.Minute)
//
// Default: Not set — exactly one idle worker is always maintained in the pool.
func DefaultIdleWorkerExpiryDuration(duration time.Duration) {
	defaultConfig.idleWorkerExpiryDuration = duration
}

// DefaultCtx sets the default context for all newly created workers.
// The context can be used to stop all worker goroutines when cancelled.
//
// Parameters:
//   - ctx: The context to use
//
// Default: nil (no context)
func DefaultCtx(ctx context.Context) {
	defaultConfig.ctx = ctx
}

func newConfig() configs {
	return defaultConfig
}

func loadConfigs(config ...any) configs {
	c := newConfig()

	return mergeConfigs(c, config...)
}

func mergeConfigs(c configs, cs ...any) configs {
	for _, config := range cs {
		switch config := config.(type) {
		case ConfigFunc:
			config(&c)
		case int:
			c.concurrency = withSafeConcurrency(config)
		}
	}

	return c
}

// WithIdleWorkerExpiryDuration configures the time period after which idle workers are
// automatically removed from the worker pool to conserve system resources.
//
// This setting helps optimize resource usage by removing unnecessary idle workers when
// the system experiences prolonged periods of low activity. When job volume increases again,
// new workers will be created as needed up to the configured concurrency level.
//
// Parameters:
//   - duration: The time period a worker can remain idle before being removed
//     (e.g., 30*time.Second, 5*time.Minute)
//
// Default behavior: If this option is not set, exactly one idle worker will always be maintained
// in the pool, regardless of how high the concurrency level is configured.
//
// When job load decreases below the concurrency level, the system will immediately begin
// removing excess idle workers according to this expiry duration setting.
func WithIdleWorkerExpiryDuration(duration time.Duration) ConfigFunc {
	return func(c *configs) {
		c.idleWorkerExpiryDuration = duration
	}
}

// WithStrategy configures the queue selection strategy for the worker.
//
// The worker uses this strategy to determine which queue to pull jobs from when multiple queues are registered.
// Available strategies are:
//   - Priority: Selects the queue with the highest priority (default)
//   - RoundRobin: Selects queues in a round-robin fashion
//   - MaxLen: Selects the queue with the most items
//   - MinLen: Selects the queue with the fewest items
//
// Parameters:
//   - strategy: The strategy to use (Priority, RoundRobin, MaxLen or MinLen)
//
// Default: If this option is not set, Priority strategy will be used.
func WithStrategy(s Strategy) ConfigFunc {
	return func(c *configs) {
		c.strategy = s
	}
}

// WithMinIdleWorkerRatio configures the minimum percentage of idle workers to maintain in the pool
// as a proportion of the total concurrency level.
//
// This configuration helps optimize resource usage by dynamically scaling the idle worker pool
// when the concurrency level changes. Maintaining some idle workers allows the system to respond
// quickly to incoming jobs without the overhead of creating new worker goroutines.
//
// Parameters:
//   - percentage: An integer between 1-100 representing the percentage of workers to keep idle
//
// Examples:
//   - WithMinIdleWorkerRatio(20): With concurrency=10, maintains 2 idle workers (20%)
//   - WithMinIdleWorkerRatio(50): With concurrency=10, maintains 5 idle workers (50%)
//
// Values outside the range 1-100 are automatically clamped (0 becomes 1, >100 becomes 100).
// By default there is always at least one idle worker inside the pool.
func WithMinIdleWorkerRatio(percentage uint8) ConfigFunc {
	return func(c *configs) {
		c.minIdleWorkerRatio = clampPercentage(percentage)
	}
}

// WithConcurrency sets the concurrency level for the worker.
// If not set, the default concurrency level is 1.
// If concurrency is less than 1, it defaults to number of CPU cores.
func WithConcurrency(concurrency int) ConfigFunc {
	return func(c *configs) {
		c.concurrency = withSafeConcurrency(concurrency)
	}
}

// WithJobIdGenerator sets the job ID generator function for the worker.
// If not set there wouldn't be any job id
func WithJobIdGenerator(fn func() string) ConfigFunc {
	return func(c *configs) {
		if fn == nil {
			return
		}
		c.jobIdGenerator = fn
	}
}

// WithContext configures the context for the worker.
//
// Each worker is associated with a context that is used to stop all goroutines when
// the worker is stopped. You can create a custom context and pass it to the worker
// to stop all workers when the context is cancelled.
//
// Parameters:
//   - ctx: The context to use
//
// Default: If this option is not set, the default is nil context.
func WithContext(ctx context.Context) ConfigFunc {
	return func(c *configs) {
		c.ctx = ctx
	}
}

func withSafeConcurrency(concurrency int) uint32 {
	// If concurrency is less than 1, use the number of CPUs as the concurrency
	if concurrency < 1 {
		return utils.Cpus()
	}
	return uint32(concurrency)
}

func clampPercentage(percentage uint8) uint8 {
	if percentage == 0 {
		return 1
	}

	if percentage > 100 {
		return 100
	}

	return percentage
}

type JobConfigFunc func(*jobConfigs)

type jobConfigs struct {
	Id string
}

func loadJobConfigs(qConfig configs, config ...JobConfigFunc) jobConfigs {
	c := jobConfigs{
		Id: qConfig.jobIdGenerator(),
	}

	for _, config := range config {
		config(&c)
	}

	return c
}

// WithJobId sets the job ID for the job.
func WithJobId(id string) JobConfigFunc {
	return func(c *jobConfigs) {
		if id == "" {
			return
		}
		c.Id = id
	}
}

type QueueConfigFunc func(*queueConfig)

type queueConfig struct {
	priority int
	capacity int
}

func loadQueueConfigs(configs ...QueueConfigFunc) queueConfig {
	c := queueConfig{
		priority: math.MaxInt,
	}
	for _, config := range configs {
		config(&c)
	}
	return c
}

// WithQueuePriority configures the priority level of a queue when binding it to a worker.
//
// The worker uses this priority to determine which queue to process jobs from first.
// Queues with a higher priority (lower integer values) will be processed before
// queues with a lower priority. In the case of ties, insertion order is preserved.
//
// Parameters:
//   - priority: An integer representing the priority level. A lower number indicates
//     a higher priority. For example, priority 1 is processed before priority 5.
//
// Examples:
//   - worker.BindQueue(varmq.WithQueuePriority(1)): Assigns a high priority to the queue.
//   - worker.BindQueue(varmq.WithQueuePriority(10)): Assigns a lower priority to the queue.
//
// Default behavior: If this option is not set, the queue implicitly receives the lowest
// possible priority (math.MaxInt).
func WithQueuePriority(priority int) QueueConfigFunc {
	return func(c *queueConfig) {
		c.priority = priority
	}
}

// WithQueueCapacity configures the maximum number of pending items a queue can hold.
//
// When the queue reaches its capacity, subsequent Add calls will return false,
// indicating the item was not enqueued. This allows callers to implement
// backpressure or overflow handling.
//
// Parameters:
//   - capacity: The maximum number of items the queue can hold.
//     Values less than 1 are treated as 0, meaning unlimited capacity.
//
// Default behavior: If this option is not set, the queue has unlimited capacity (0).
func WithQueueCapacity(capacity int) QueueConfigFunc {
	return func(c *queueConfig) {
		c.capacity = max(0, capacity)
	}
}
