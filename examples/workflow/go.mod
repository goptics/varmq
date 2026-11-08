module github.com/goptics/varmq/examples/workflow

go 1.27

require github.com/goptics/varmq v0.0.0-00010101000000-000000000000

require github.com/goptics/varmq/contract v0.3.0 // indirect

// Local paths: run from this repo.
replace github.com/goptics/varmq => ../../

replace github.com/goptics/varmq/contract => ../../contract
