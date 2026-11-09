module github.com/goptics/varmq/examples/redis-distributed

go 1.27

require (
	github.com/goptics/redisq v1.5.2-0.20261109084156-7debbc2bbc5b
	github.com/goptics/varmq v1.0.0
	github.com/matoous/go-nanoid/v2 v2.1.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/goptics/varmq/contract v0.8.0 // indirect
	github.com/redis/go-redis/v9 v9.23.0 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

// Replace the remote module with your local path
// Assuming your project structure has examples/distributed at the same level as your main module
replace github.com/goptics/varmq => ../../

replace github.com/goptics/varmq/contract => ../../contract
