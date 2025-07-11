package varmq

import (
	"time"

	"github.com/goptics/varmq/utils"
)

// ConfigFunc is a function that configures a worker.
type ConfigFunc func(*configs)

type configs struct {
	concurrency              uint32
	jobIdGenerator           func() string
	idleWorkerExpiryDuration time.Duration
	minIdleWorkerRatio       uint8
	strategy                 Strategy
}

func newConfig() configs {
	return configs{
		concurrency: 1,
		jobIdGenerator: func() string {
			return ""
		},
		strategy: RoundRobin,
	}
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
//   - RoundRobin: Selects queues in a round-robin fashion (default)
//   - MaxLen: Selects the queue with the most items
//   - MinLen: Selects the queue with the fewest items
//
// Parameters:
//   - strategy: The strategy to use (RoundRobin, MaxLen or  MinLen)
//
// Default: If this option is not set, RoundRobin strategy will be used.
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
		c.jobIdGenerator = fn
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
