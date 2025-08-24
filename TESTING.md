# Testing Guide

ZMQ4 uses comprehensive black-box testing to ensure protocol compliance and reliability.

## Test Structure

### Unit Tests
Located in `unit_test/` with protocol-specific directories:

```
unit_test/
├── zyre_test/          # Zyre protocol tests
├── dafka_test/         # Dafka protocol tests  
└── malamute_test/      # Malamute protocol tests
```

### Test Coverage
- **Zyre Protocol**: 8 test functions, 16 scenarios
- **Dafka Protocol**: 8 test functions, 18 scenarios  
- **Malamute Protocol**: 10 test functions, 25 scenarios

**Total**: 26 test functions, 59 test scenarios

## Running Tests

### All Tests
```bash
make test
```

### Protocol-Specific Tests
```bash
# Test individual protocols
make test-zyre
make test-dafka
make test-malamute

# Run with coverage
make test-coverage
```

### Individual Test Execution
```bash
# From project root
cd unit_test/zyre_test && go test -v
cd unit_test/dafka_test && go test -v  
cd unit_test/malamute_test && go test -v
```

## Test Scenarios

### Zyre Protocol Tests
1. **Network Operations**
   - Network interface discovery
   - Beacon transmission
   - UDP communication

2. **Peer Management**
   - Peer discovery
   - Connection handling
   - Heartbeat monitoring

3. **Group Messaging**
   - Group join/leave
   - Message broadcasting
   - Event handling

### Dafka Protocol Tests  
1. **Producer Operations**
   - Message publishing
   - Topic management
   - Credit handling

2. **Consumer Operations**
   - Message consumption
   - Subscription management
   - Flow control

3. **System Integration**
   - Producer-consumer coordination
   - Message ordering
   - Persistence handling

### Malamute Protocol Tests
1. **Client Operations**
   - Connection management
   - Request-reply patterns
   - Stream handling

2. **Worker Operations**
   - Service registration
   - Request processing
   - Response handling

3. **Broker Operations**
   - Message routing
   - Service discovery
   - Connection multiplexing

## Black-Box Testing Approach

Tests validate external behavior without knowledge of internal implementation:

- **Protocol Compliance**: Verify message formats and flows
- **Error Handling**: Test failure scenarios and recovery
- **Performance**: Measure throughput and latency
- **Concurrency**: Validate thread-safe operations

## Test Dependencies

Required packages for testing:
- `github.com/stretchr/testify` - Assertion framework
- `go.uber.org/goleak` - Goroutine leak detection
- Internal testutil packages for helper functions

## Continuous Integration

Tests run automatically on:
- Pull requests
- Main branch commits
- Release candidates

Expected result: **All tests must pass** (59/59 scenarios)

## Test Report Format

```
=== Test Execution Report ===
Protocol: [Zyre|Dafka|Malamute]
Functions: X/X passed
Scenarios: Y/Y passed
Status: ✅ SUCCESS
Duration: Xs
```

## Contributing Tests

When adding new functionality:
1. Create corresponding test scenarios
2. Follow black-box testing principles
3. Ensure all tests pass before submitting
4. Update test documentation