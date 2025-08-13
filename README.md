# zmq4

[![GitHub release](https://img.shields.io/github/release/destiny/zmq4.svg)](https://github.com/destiny/zmq4/releases)
[![go.dev reference](https://pkg.go.dev/badge/github.com/destiny/zmq4)](https://pkg.go.dev/github.com/destiny/zmq4)
[![CI](https://github.com/destiny/zmq4/workflows/CI/badge.svg)](https://github.com/destiny/zmq4/actions)
[![codecov](https://codecov.io/gh/destiny/zmq4/branch/main/graph/badge.svg)](https://codecov.io/gh/destiny/zmq4)
[![GoDoc](https://godoc.org/github.com/destiny/zmq4?status.svg)](https://godoc.org/github.com/destiny/zmq4)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/destiny/zmq4)](https://goreportcard.com/report/github.com/destiny/zmq4)

`zmq4` is a pure-Go implementation of ØMQ (ZeroMQ), version 4.

See [zeromq.org](http://zeromq.org) for more informations.

## Fork Information

This is an enhanced fork of the original [go-zeromq/zmq4](https://github.com/go-zeromq/zmq4) project with additional improvements and active maintenance.

## Quick Start

```bash
go get github.com/destiny/zmq4
```

## Development

Use the provided Makefile for common development tasks:

```bash
make help          # Show available targets
make build         # Build all packages
make test          # Run tests
make test-race     # Run tests with race detection
make lint          # Run linter
make examples      # Build all examples
```

## Examples

Examples are located in the `example/` directory, each in its own subdirectory:

- `example/hwclient/` - Hello World client
- `example/hwserver/` - Hello World server
- `example/curve-client/` - CURVE security client
- `example/curve-server/` - CURVE security server

Run examples:
```bash
make run-hwserver    # Terminal 1
make run-hwclient    # Terminal 2
```

## License

`zmq4` is released under the `Apache 2.0` license.

## Documentation

Documentation for `zmq4` is served by [GoDoc](https://godoc.org/github.com/destiny/zmq4).


