module github.com/goptics/varmq

go 1.27

require github.com/stretchr/testify v1.11.1

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/goptics/varmq/contract v0.3.0
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// ponytail: replace is for this repo only; users get the required version, so
// require must name a published contract/vX tag before a varmq release.
replace github.com/goptics/varmq/contract => ./contract
