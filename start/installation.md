# Installation

Add VarMQ to a Go module.

VarMQ needs Go 1.24 or newer. Add it to your module with `go get`, then import it. Pick the variant you need:

The in-memory worker pool, queues and topics are all in this one package. Persistent and distributed queues also need a storage adapter, which is its own module: [redisq](https://github.com/goptics/redisq) for Redis or [sqliteq](https://github.com/goptics/sqliteq) for SQLite.

## Versions

Each module has its own `go.mod`, so each one can need a different Go version. The table is for redisq v1.5.0 and sqliteq v0.3.0.

| Module | Go version | Other needs |
|---|---|---|
| `github.com/goptics/varmq` | 1.24 or newer | None. |
| `github.com/goptics/redisq` v1.5.0 | 1.26 or newer | Redis 6.0 or newer, with Lua scripts allowed. |
| `github.com/goptics/sqliteq` v0.3.0 | 1.24 or newer | cgo (`CGO_ENABLED=1`) and a C compiler, because it uses `mattn/go-sqlite3`. |

Neither adapter lists `github.com/goptics/varmq` in its `go.mod`. So an adapter does not set the VarMQ version; your own module does.

Next: the [quick start](https://varmq.goptics.org/start/quick-start/).
