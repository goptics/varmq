package gocq

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
		c.Id = id
	}
}
