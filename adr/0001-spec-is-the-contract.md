# The spec in `spec/` is the contract, not the Go code

varmq, its storage backends (redisq) and its observers (vartop) connect through a versioned spec in this repository, not through Go imports: `spec/openapi.yaml` for the HTTP API that `varmq.Handler` serves (`/workers`, `/queues`, `/queues/{id}/jobs`), and `spec/redis-storage.md` for the Redis key layout, the JSON Job Record and the rules each script follows. varmq-ts and varmq-rs will move into this repository and implement the same spec, so a TypeScript producer can feed a Go consumer on one Redis queue and vartop reads any of them unchanged. Go types are a copy of the spec, never its source.

## Considered Options

- **A Go-only contract module**: rejected; other languages could not use it.
- **A separate `goptics/spec` repository**: rejected; every implementation lives here, so a second repository only adds release coordination.

## Consequences

- A change to the HTTP API or the Redis layout starts as a spec change with a version bump; implementations follow.
- vartop never reads Redis directly. Queue data reaches it through `/queues`, grouped by Queue ID so a shared queue is not counted once per instance.
