# zmq4

[![GitHub release](https://img.shields.io/github/release/destiny/zmq4.svg)](https://github.com/destiny/zmq4/releases)
[![go.dev reference](https://pkg.go.dev/badge/github.com/destiny/zmq4/v25)](https://pkg.go.dev/github.com/destiny/zmq4/v25)
[![CI](https://github.com/destiny/zmq4/workflows/CI/badge.svg)](https://github.com/destiny/zmq4/actions)
[![codecov](https://codecov.io/gh/destiny/zmq4/branch/main/graph/badge.svg)](https://codecov.io/gh/destiny/zmq4)
[![GoDoc](https://godoc.org/github.com/destiny/zmq4/v25?status.svg)](https://godoc.org/github.com/destiny/zmq4/v25)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Report Card](https://goreportcard.com/badge/github.com/destiny/zmq4)](https://goreportcard.com/report/github.com/destiny/zmq4)

`zmq4` is a pure-Go implementation of ØMQ (ZeroMQ), version 4.

See [zeromq.org](http://zeromq.org) for more informations.

## Fork Information

This is an enhanced fork of the original [go-zeromq/zmq4](https://github.com/go-zeromq/zmq4) project with additional improvements and active maintenance.

## Installation

To use the latest stable version of zmq4 in your Go project:

```bash
go get github.com/destiny/zmq4/v25
```

### Versioning Scheme

This project uses a **YY.Q.NN** versioning format:
- **YY**: Year (e.g., 25 for 2025)
- **Q**: Quarter (1-4)
- **NN**: Sequence number within the quarter

Example: `v25.3.2` means the 2nd release in Q3 of 2025.

### Import in Your Code

```go
import "github.com/destiny/zmq4/v25"

// Or with alias
import zmq "github.com/destiny/zmq4/v25"
```

### Subpackages

For specific functionality, import the relevant subpackages:

```go
import "github.com/destiny/zmq4/v25/security/curve"  // CURVE security
import "github.com/destiny/zmq4/v25/majordomo"      // Majordomo Pattern
import "github.com/destiny/zmq4/v25/z85"            // Z85 encoding
```

## Quick Example

Here's a simple client-server example:

```go
package main

import (
    "context"
    "fmt"
    
    "github.com/destiny/zmq4/v25"
)

func main() {
    // Server
    ctx := context.Background()
    server := zmq4.NewRep(ctx)
    defer server.Close()
    
    go func() {
        server.Listen("tcp://127.0.0.1:5555")
        for {
            msg, _ := server.Recv()
            server.Send(zmq4.NewMsgString("Hello " + string(msg.Bytes())))
        }
    }()
    
    // Client
    client := zmq4.NewReq(ctx)
    defer client.Close()
    
    client.Dial("tcp://127.0.0.1:5555")
    client.Send(zmq4.NewMsgString("World"))
    reply, _ := client.Recv()
    fmt.Println(string(reply.Bytes())) // Outputs: Hello World
}
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

Documentation for `zmq4` is served by [GoDoc](https://godoc.org/github.com/destiny/zmq4/v25).


