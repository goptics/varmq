package varmq

type JobConfigFunc func(*jobConfigs)

type jobConfigs struct {
	Id string
}

func loadJobConfigs(qConfig configs, config ...JobConfigFunc) jobConfigs {
	c := jobConfigs{
		Id: qConfig.JobIdGenerator(),
	}

	for _, config := range config {
		config(&c)
	}

	return c
}

func WithJobId(id string) JobConfigFunc {
	return func(c *jobConfigs) {
		if id == "" {
			return
		}
		c.Id = id
	}
}

func withRequiredJobId(c jobConfigs) jobConfigs {
	if c.Id == "" {
		panic("job id is required for persistent queue")
	}

	return c
}
