.PHONY: help build test test-race test-coverage clean lint fmt vet examples install deps test-unit test-e2e test-security test-performance test-all test-zyre test-dafka test-malamute test-smoke test-curve test-plain test-null test-regression coverage-comprehensive

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

# ZMQ4 Test Suite

# Unit Tests (Working Components Only)
test-unit: ## Run all unit tests with black box approach
	@echo "Running unit tests (black box testing)..."
	cd unit_test/zyre_test && go test $(GOFLAGS) $(TAGS) -race .
	cd unit_test/dafka_test && go test $(GOFLAGS) $(TAGS) -race .
	cd unit_test/malamute_test && go test $(GOFLAGS) $(TAGS) -race .

# Individual Protocol Tests
test-zyre: ## Run Zyre protocol unit tests
	@echo "Running Zyre protocol tests..."
	cd unit_test/zyre_test && go test $(GOFLAGS) $(TAGS) -race -v .

test-dafka: ## Run Dafka protocol unit tests
	@echo "Running Dafka protocol tests..."
	cd unit_test/dafka_test && go test $(GOFLAGS) $(TAGS) -race -v .

test-malamute: ## Run Malamute protocol unit tests
	@echo "Running Malamute protocol tests..."
	cd unit_test/malamute_test && go test $(GOFLAGS) $(TAGS) -race -v .

# Security Tests (Built-in security components)
test-curve: ## Run CURVE security mechanism tests
	@echo "Running CURVE security tests..."
	go test $(GOFLAGS) $(TAGS) -v ./security/curve/

test-plain: ## Run PLAIN security mechanism tests
	@echo "Running PLAIN security tests..."
	go test $(GOFLAGS) $(TAGS) -v ./security/plain/

test-null: ## Run NULL security mechanism tests
	@echo "Running NULL security tests..."
	go test $(GOFLAGS) $(TAGS) -v ./security/null/

test-security: ## Run all security mechanism tests
	@echo "Running security tests..."
	@make test-curve
	@make test-plain  
	@make test-null

# Core Component Tests
test-core: ## Run core ZMQ4 component tests
	@echo "Running core component tests..."
	go test $(GOFLAGS) $(TAGS) -race ./

# Comprehensive Test Suite  
test-all: ## Run complete working test suite
	@echo "Running comprehensive test suite..."
	@make test-core
	@make test-unit
	@make test-security
	@echo "✅ All test suites completed successfully"

# Quick Development Tests
test-smoke: ## Run quick smoke tests for development
	@echo "Running smoke tests..."
	cd unit_test/zyre_test && go test $(GOFLAGS) $(TAGS) -short .
	cd unit_test/dafka_test && go test $(GOFLAGS) $(TAGS) -short .
	cd unit_test/malamute_test && go test $(GOFLAGS) $(TAGS) -short .

# Test Coverage Report
test-coverage-unit: ## Generate test coverage report for unit tests
	@echo "Generating unit test coverage report..."
	cd unit_test/zyre_test && go test -coverprofile=zyre-coverage.out $(TAGS) .
	cd unit_test/dafka_test && go test -coverprofile=dafka-coverage.out $(TAGS) .
	cd unit_test/malamute_test && go test -coverprofile=malamute-coverage.out $(TAGS) .