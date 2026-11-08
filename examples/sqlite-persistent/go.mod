module github.com/goptics/varmq/examples/sqlite-persistent

go 1.27

require (
	github.com/goptics/sqliteq v0.3.0
	github.com/goptics/varmq v0.0.0-00010101000000-000000000000
)

require (
	github.com/goptics/varmq/contract v0.3.0 // indirect
	github.com/mattn/go-sqlite3 v1.14.28 // indirect
)

// Replace the remote module with your local path
// Assuming your project structure has examples/distributed at the same level as your main module
replace github.com/goptics/varmq => ../../

replace github.com/goptics/varmq/contract => ../../contract
