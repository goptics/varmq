module github.com/goptics/varmq/examples/sqlite-persistent

go 1.27

require (
	github.com/goptics/sqliteq v0.3.1-0.20261109084420-6550e3caed36
	github.com/goptics/varmq v1.10.1-0.20261108161945-6bc553db29c6
)

require (
	github.com/goptics/varmq/contract v0.8.0 // indirect
	github.com/mattn/go-sqlite3 v1.14.28 // indirect
)

// Replace the remote module with your local path
// Assuming your project structure has examples/distributed at the same level as your main module
replace github.com/goptics/varmq => ../../

replace github.com/goptics/varmq/contract => ../../contract
