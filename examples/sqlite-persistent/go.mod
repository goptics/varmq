module github.com/goptics/varmq/examples/sqlite-persistent

go 1.24.2

require (
	github.com/goptics/sqliteq v0.2.1
	github.com/goptics/varmq v0.0.0-00010101000000-000000000000
	github.com/lucsky/cuid v1.2.1
)

require github.com/mattn/go-sqlite3 v1.14.28 // indirect

// Replace the remote module with your local path
// Assuming your project structure has examples/distributed at the same level as your main module
replace github.com/goptics/varmq => ../../
