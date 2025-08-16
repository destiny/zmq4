.PHONY: help build test test-race test-coverage clean lint fmt vet examples install deps

# Default Go flags
GOFLAGS ?= -v
TAGS ?= -tags=czmq4

# Help target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Development targets
build: ## Build all packages
	go build $(GOFLAGS) $(TAGS) ./...

install: ## Install all packages
	go install $(GOFLAGS) $(TAGS) ./...

test: ## Run tests
	go test $(GOFLAGS) $(TAGS) ./...

test-race: ## Run tests with race detection
	go test -race $(GOFLAGS) $(TAGS) ./...

test-coverage: ## Run tests with coverage
	go test -race -coverprofile=coverage.out -covermode=atomic $(TAGS) ./...
	go tool cover -html=coverage.out -o coverage.html

test-pebbe: ## Run head-to-head tests against pebbe/zmq4 (requires libzmq)
	go test $(GOFLAGS) -tags=pebbe ./security/curve -run TestCURVE.*Interop

test-signature-fix: ## Test the signature box fix with fresh keys
	go test $(GOFLAGS) $(TAGS) . -run TestCURVESignatureBoxFix

test-interop: ## Run all interoperability tests (czmq4 + pebbe)
	go test $(GOFLAGS) $(TAGS) . -run TestCURVEInterop
	go test $(GOFLAGS) -tags=pebbe ./security/curve -run TestCURVE.*Interop

bench: ## Run benchmarks
	go test -bench=. -benchmem $(TAGS) ./...

# Code quality targets
fmt: ## Format code
	gofmt -w -s .

vet: ## Run go vet
	go vet $(TAGS) ./...

lint: ## Run linter (requires golangci-lint)
	golangci-lint run

# Examples targets
examples: ## Build all examples
	@for dir in example/*/; do \
		echo "Building $$dir"; \
		(cd "$$dir" && go build $(GOFLAGS) $(TAGS) .); \
	done

run-hwserver: ## Run hello world server example
	cd example/hwserver && go run $(TAGS) main.go

run-hwclient: ## Run hello world client example
	cd example/hwclient && go run $(TAGS) main.go

# Maintenance targets
deps: ## Download dependencies
	go mod download
	go mod tidy

clean: ## Clean build artifacts
	go clean -cache -testcache
	rm -f coverage.out coverage.html

# CI targets
ci: deps fmt vet lint test-race ## Run full CI pipeline

# Check if tools are available
check-tools: ## Check if required tools are installed
	@command -v golangci-lint >/dev/null 2>&1 || echo "Warning: golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"