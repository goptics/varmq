module github.com/goptics/varmq/examples/redis-persistent

go 1.24.1

require (
	github.com/goptics/redisq v1.3.1
	github.com/goptics/varmq v1.0.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/lucsky/cuid v1.2.1 // indirect
	github.com/redis/go-redis/v9 v9.7.3 // indirect
)

// Replace the remote module with your local path
// Assuming your project structure has examples/distributed at the same level as your main module
replace github.com/goptics/varmq => ../../
