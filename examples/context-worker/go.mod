module github.com/goptics/varmq/examples/context-worker

go 1.25.6

// Replace the remote module with your local path
// Assuming your project structure has examples/distributed at the same level as your main module
replace github.com/goptics/varmq => ../../

require github.com/goptics/varmq v0.0.0-00010101000000-000000000000
