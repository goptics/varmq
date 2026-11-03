.PHONY: help init test bench format diagrams release

# Default target - list available targets
help:
	@echo "Available targets:"
	@echo "  help     - List available targets"
	@echo "  init     - Install dependencies and setup git hooks"
	@echo "  test     - Run all tests (no cache)"
	@echo "  format   - Format codes"
	@echo "  diagrams - Regenerate animated README diagrams (assets/diagrams/gen)"
	@echo "  release  - Create and push a new tag to trigger release (usage: make release VERSION=v1.0.0)"

# Install dependencies and setup git hooks
init:
	go install github.com/evilmartians/lefthook@latest
	go mod tidy
	go mod vendor
	lefthook install
	@echo "✅ Dependencies installed and git hooks activated via lefthook"

# Run all tests (no cache)
test:
	go test -race -count=1 -timeout 5m -v ./...

bench:
	go test -bench=. -timeout 5m -benchmem -cpu=1

# Format codes
format:
	gofmt -w .

# Regenerate animated README diagrams
diagrams:
	cd assets/diagrams/gen && for d in single-worker:single worker-pool:pool multi-queue:multi priority-queue:priority topic:topic batch:batch lifecycle:lifecycle; do \
		python3 gen_$${d#*:}.py $${d%:*}.tpl.svg ../varmq-$${d%:*}.svg > /dev/null || exit 1; \
	done

# Create and push a new tag to trigger release
release:
ifndef VERSION
	$(error VERSION is required. Usage: make release VERSION=v1.0.0)
endif
	@echo "This will create and push a new release tag. Continue? [y/N]" && read ans && [ $${ans:-N} = y ]
	@echo "Creating tag $(VERSION)..."
	git tag -s $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)
	@echo "Tag $(VERSION) pushed. GitHub Actions will create the release."
