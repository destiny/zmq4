# Development Guide

Guide for contributing to and developing ZMQ4.

## Prerequisites

- **Go**: Version 1.19 or later
- **Make**: For build automation
- **Git**: For version control

## Development Environment Setup

1. **Clone the repository**
   ```bash
   git clone https://github.com/destiny/zmq4.git
   cd zmq4
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Verify setup**
   ```bash
   make test
   ```

## Project Structure

```
zmq4/
├── README.md              # Project overview
├── go.mod                # Go module definition
├── zmq4.go               # Core ØMQ implementation
├── socket_*.go           # Socket implementations
├── security/             # Security mechanisms
│   ├── curve/           # CURVE security
│   ├── plain/           # PLAIN authentication
│   └── null/            # NULL security
├── zyre/                # Zyre proximity protocol
├── dafka/               # Dafka streaming protocol
├── malamute/            # Malamute enterprise messaging
├── majordomo/           # Majordomo service pattern
├── internal/            # Internal utilities
├── unit_test/           # Test suite
├── example/             # Example programs
└── docs/                # Documentation
```

## Build System

### Available Make Targets

```bash
make help          # Show all available targets
make build         # Build all packages
make test          # Run all tests
make test-race     # Run tests with race detection
make test-coverage # Run tests with coverage
make lint          # Run code quality checks
make examples      # Build example programs
make clean         # Clean build artifacts
```

### Custom Build Flags

```bash
# Build with race detection
go build -race ./...

# Build with debug symbols
go build -gcflags="all=-N -l" ./...

# Cross-compilation
GOOS=linux GOARCH=amd64 go build ./...
```

## Code Guidelines

### Go Standards
- Follow official Go formatting (`gofmt`)
- Use meaningful variable and function names
- Write comprehensive documentation for public APIs
- Handle errors explicitly

### Package Organization
- Keep packages focused and cohesive
- Minimize dependencies between packages
- Use internal packages for implementation details
- Export only necessary types and functions

### Testing Requirements
- Write black-box tests for all public functionality
- Achieve high test coverage (aim for >90%)
- Test error conditions and edge cases
- Use table-driven tests where appropriate

### Security Considerations
- Never log sensitive information (keys, passwords)
- Validate all input data
- Use secure random number generation
- Follow cryptographic best practices

## Protocol Implementation

### Adding New Protocols

1. **Create protocol package**
   ```bash
   mkdir newprotocol/
   ```

2. **Implement core types**
   ```go
   type Client struct { ... }
   type Server struct { ... }
   type Message struct { ... }
   ```

3. **Add comprehensive tests**
   ```bash
   mkdir unit_test/newprotocol_test/
   ```

4. **Update documentation**
   - Add to PROTOCOLS.md
   - Update README.md
   - Create examples

### Protocol Design Principles
- Follow ØMQ patterns and conventions
- Implement proper error handling
- Support concurrent operations
- Provide clean, idiomatic APIs

## Testing Strategy

### Test Categories
1. **Unit Tests**: Individual component testing
2. **Integration Tests**: Multi-component interaction
3. **Performance Tests**: Throughput and latency
4. **Security Tests**: Encryption and authentication

### Running Specific Tests
```bash
# Test single package
go test ./zyre/

# Test with verbose output
go test -v ./unit_test/zyre_test/

# Test with coverage
go test -cover ./...

# Benchmark tests
go test -bench=. ./...
```

## Debugging

### Debug Builds
```bash
go build -gcflags="all=-N -l" ./...
```

### Logging
Enable verbose logging in protocols:
```go
broker.SetVerbose(true)
node.SetVerbose(true)
```

### Profiling
```bash
go test -cpuprofile=cpu.prof -memprofile=mem.prof ./...
go tool pprof cpu.prof
```

## Performance Optimization

### Best Practices
- Minimize memory allocations
- Reuse buffers where possible
- Use connection pooling
- Implement proper backpressure

### Benchmarking
```bash
go test -bench=BenchmarkProtocol ./...
go test -benchmem -bench=. ./...
```

## Release Process

1. **Update version numbers**
2. **Run full test suite**
3. **Update CHANGELOG.md**
4. **Create git tag**
5. **Publish release**

### Version Scheme
Follow semantic versioning (SemVer):
- MAJOR: Breaking changes
- MINOR: New features (backward compatible)
- PATCH: Bug fixes

## Contributing Workflow

1. **Fork the repository**
2. **Create feature branch**
   ```bash
   git checkout -b feature/amazing-feature
   ```
3. **Make changes**
4. **Add tests**
5. **Run test suite**
   ```bash
   make test
   ```
6. **Commit changes**
   ```bash
   git commit -m "Add amazing feature"
   ```
7. **Push to fork**
8. **Open Pull Request**

## Code Review Process

All changes require:
- Code review by maintainers
- All tests passing
- Documentation updates
- Backward compatibility (unless major version)

## Getting Help

- **Issues**: GitHub Issues for bugs and feature requests
- **Discussions**: GitHub Discussions for questions
- **Documentation**: Check README.md and docs/