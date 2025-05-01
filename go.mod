module github.com/goptics/varmq

go 1.24.0

require github.com/stretchr/testify v1.10.0

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

retract (
    // Old versions from github.com/fahimfaisaal/gocq
    v2.0.0 // Moving to github.com/goptics/varmq
    v1.0.0 // Moving to github.com/goptics/varmq
)
