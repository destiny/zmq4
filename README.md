# ZMQ4 - Pure Go Implementation of ØMQ 4.x

[![Go Report Card](https://goreportcard.com/badge/github.com/destiny/zmq4)](https://goreportcard.com/report/github.com/destiny/zmq4)
[![GoDoc](https://godoc.org/github.com/destiny/zmq4/v25?status.svg)](https://godoc.org/github.com/destiny/zmq4/v25)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**ZMQ4** is a comprehensive, production-ready pure-Go implementation of ØMQ (ZeroMQ) version 4.x with complete protocol support, security features, and high-level messaging patterns.

## 🚀 Key Features

### ✅ **Complete Protocol Support**
- **Core ØMQ Patterns**: REQ/REP, DEALER/ROUTER, PUB/SUB, PUSH/PULL, PAIR, XPUB/XSUB
- **High-Level Protocols**: Zyre (proximity P2P), Dafka (decentralized streaming), Malamute (enterprise messaging)
- **Majordomo Pattern (MDP)**: Service-oriented architecture with brokers, workers, and clients

### ✅ **Enterprise Security**
- **CURVE Security**: Elliptic curve cryptography with Curve25519
- **PLAIN Authentication**: Username/password authentication
- **NULL Security**: No authentication (development/testing)
- Complete handshake protocols with anti-amplification protection

### ✅ **Production Ready**
- Pure Go implementation - no C dependencies
- Comprehensive test coverage with black-box testing
- Memory-efficient and high-performance
- Cross-platform compatibility (Linux, macOS, Windows)
- Thread-safe concurrent operations

## 📦 Installation

```bash
go get github.com/destiny/zmq4/v25
```

### Import in Your Code

```go
import "github.com/destiny/zmq4/v25"

// For specific protocols
import "github.com/destiny/zmq4/v25/zyre"      // Proximity P2P
import "github.com/destiny/zmq4/v25/dafka"     // Decentralized streaming  
import "github.com/destiny/zmq4/v25/malamute"  // Enterprise messaging
import "github.com/destiny/zmq4/v25/majordomo" // Service-oriented pattern
import "github.com/destiny/zmq4/v25/security/curve" // CURVE security
```

## 🏁 Quick Start

### Basic Request-Reply Pattern

```go
package main

import (
    "context"
    "fmt"
    "github.com/destiny/zmq4/v25"
)

func main() {
    ctx := context.Background()
    
    // Server
    server := zmq4.NewRep(ctx)
    defer server.Close()
    
    go func() {
        server.Listen("tcp://127.0.0.1:5555")
        for {
            msg, _ := server.Recv()
            reply := zmq4.NewMsgString("Hello " + string(msg.Bytes()))
            server.Send(reply)
        }
    }()
    
    // Client
    client := zmq4.NewReq(ctx)
    defer client.Close()
    
    client.Dial("tcp://127.0.0.1:5555")
    client.Send(zmq4.NewMsgString("World"))
    
    reply, _ := client.Recv()
    fmt.Println(string(reply.Bytes())) // Output: Hello World
}
```

### Secure Communication with CURVE

```go
import "github.com/destiny/zmq4/v25/security/curve"

// Generate key pairs
serverPublic, serverPrivate, _ := curve.GenerateKeypair()
clientPublic, clientPrivate, _ := curve.GenerateKeypair()

// Server with CURVE security
serverSecurity := curve.NewSecurity("", serverPublic, serverPrivate)
server := zmq4.NewRep(ctx, zmq4.WithSecurity(serverSecurity))

// Client with CURVE security  
clientSecurity := curve.NewSecurity(serverPublic, clientPublic, clientPrivate)
client := zmq4.NewReq(ctx, zmq4.WithSecurity(clientSecurity))
```

## 🌐 High-Level Protocols

### Zyre - Proximity Peer-to-Peer

Zyre enables automatic peer discovery and group messaging for local networks:

```go
import "github.com/destiny/zmq4/v25/zyre"

// Create node
node, _ := zyre.NewNode(&zyre.NodeConfig{
    Name: "my-node",
    Headers: map[string]string{"service": "chat"},
})

// Start and join group
node.Start()
node.Join("CHAT")

// Send group message
node.Shout("CHAT", []byte("Hello everyone!"))

// Handle events
for event := range node.Events() {
    switch event.Type() {
    case zyre.EventEnter:
        fmt.Printf("Peer %s entered\n", event.PeerName())
    case zyre.EventShout:
        fmt.Printf("Group message: %s\n", string(event.Msg()))
    }
}
```

### Dafka - Decentralized Streaming

Dafka provides distributed publish-subscribe with persistence and ordering:

```go
import "github.com/destiny/zmq4/v25/dafka"

// Producer
producer, _ := dafka.NewProducer("tcp://127.0.0.1:5556")
producer.SetTopic("events")
producer.SetPublisherID("publisher-1")

message := dafka.NewMessage("events", []byte("Event data"))
producer.Publish(message)

// Consumer
consumer, _ := dafka.NewConsumer("tcp://127.0.0.1:5556")  
consumer.Subscribe("events")

for msg := range consumer.Messages() {
    fmt.Printf("Received: %s\n", string(msg.Content()))
}
```

### Malamute - Enterprise Messaging

Malamute enables service-oriented architecture with streams and services:

```go
import "github.com/destiny/zmq4/v25/malamute"

// Broker
broker, _ := malamute.NewBroker("tcp://*:5555")
broker.SetVerbose(true)
go broker.Start()

// Worker providing a service
worker, _ := malamute.NewWorker("tcp://127.0.0.1:5555", "calculator")
go func() {
    for request := range worker.Requests() {
        result := calculate(request.Content())
        reply := malamute.NewMessage("result", result)
        worker.Reply(reply)
    }
}()

// Client using the service
client, _ := malamute.NewClient("tcp://127.0.0.1:5555")
request := malamute.NewMessage("calculator", []byte("2+2"))
reply, _ := client.Request(request)
fmt.Printf("Result: %s\n", string(reply.Content()))
```

## 🎯 Choosing the Right Pattern

| Pattern | Use Case | Communication |
|---------|----------|---------------|
| **REQ/REP** | Client-server, synchronous | 1:1 request-reply |
| **DEALER/ROUTER** | Async request-reply, load balancing | N:M with routing |
| **PUB/SUB** | Broadcasting, events | 1:N publish-subscribe |
| **PUSH/PULL** | Pipeline processing | N:M load distribution |
| **PAIR** | Exclusive connection | 1:1 bidirectional |
| **Zyre** | P2P discovery, local networks | N:N proximity-based |
| **Dafka** | Event streaming, persistence | N:N with ordering |
| **Malamute** | SOA, enterprise integration | N:M service-oriented |
| **Majordomo** | Service discovery, reliability | N:M with brokering |

## 🔒 Security

### CURVE Security Protocol

CURVE provides strong security using elliptic curve cryptography:

- **Perfect Forward Secrecy**: Each session uses unique keys
- **Identity Verification**: Public key authentication
- **Anti-Replay Protection**: Nonce-based message ordering
- **Zero-Configuration**: Automatic key exchange

```go
// Server setup
serverKeys, _ := curve.GenerateKeypair()
serverSecurity := curve.NewSecurity("", serverKeys.Public, serverKeys.Private)

// Client setup  
clientKeys, _ := curve.GenerateKeypair()
clientSecurity := curve.NewSecurity(serverKeys.Public, clientKeys.Public, clientKeys.Private)

// Use with any socket type
server := zmq4.NewRep(ctx, zmq4.WithSecurity(serverSecurity))
client := zmq4.NewReq(ctx, zmq4.WithSecurity(clientSecurity))
```

## 🧪 Testing

The project includes comprehensive black-box testing for all protocols:

```bash
# Run all unit tests
make test-unit

# Test specific protocols
make test-zyre
make test-dafka  
make test-malamute

# Run with coverage
make test-coverage
```

### Test Coverage
- **Zyre Protocol**: 16 test scenarios covering peer discovery, group messaging, events
- **Dafka Protocol**: 18 test scenarios covering producers, consumers, credit flow control
- **Malamute Protocol**: 25 test scenarios covering clients, workers, brokers, streams

## 🛠️ Development

### Prerequisites
- Go 1.19+ 
- Make (for build automation)

### Build Commands

```bash
make help          # Show available targets
make build         # Build all packages  
make test          # Run tests
make test-race     # Run tests with race detection
make lint          # Run code quality checks
make examples      # Build example programs
```

### Project Structure

```
zmq4/
├── README.md           # This file
├── zmq4.go            # Core ØMQ implementation
├── socket_*.go        # Socket type implementations
├── security/          # Security implementations
│   ├── curve/        # CURVE security
│   ├── plain/        # PLAIN authentication  
│   └── null/         # NULL security
├── zyre/             # Zyre proximity protocol
├── dafka/            # Dafka streaming protocol
├── malamute/         # Malamute enterprise messaging
├── majordomo/        # Majordomo service pattern
├── unit_test/        # Comprehensive test suite
└── example/          # Example programs
```

## 📚 Examples

Comprehensive examples are provided in the `example/` directory:

### Basic Patterns
- **Hello World**: `example/hwserver/`, `example/hwclient/`
- **Publish-Subscribe**: `example/psenvpub/`, `example/psenvsub/`
- **Pipeline**: `example/rtdealer/`

### Security Examples  
- **CURVE Security**: `example/curve-server/`, `example/curve-client/`

### Service Patterns
- **Majordomo**: `example/mdp-broker/`, `example/mdp-worker/`, `example/mdp-client/`
- **Secure MDP**: `example/mdp-secure-*/`

### High-Level Protocols
- **Zyre**: `example/zyre-chat/`, `example/zyre-discovery/`

### Running Examples

```bash
# Basic hello world
cd example/hwserver && go run main.go    # Terminal 1
cd example/hwclient && go run main.go    # Terminal 2

# Secure communication
cd example/curve-server && go run main.go    # Terminal 1  
cd example/curve-client && go run main.go    # Terminal 2

# Build all examples
make examples
```

## 🔧 Advanced Configuration

### Socket Options

```go
// Configure socket options
socket := zmq4.NewReq(ctx)
socket.SetOption(zmq4.OptionSndHWM, 1000)      // Send high water mark
socket.SetOption(zmq4.OptionRcvHWM, 1000)      // Receive high water mark  
socket.SetOption(zmq4.OptionLinger, time.Second) // Linger time
socket.SetOption(zmq4.OptionIdentity, "client-1") // Socket identity
```

### Connection Management

```go
// Multiple connections
socket := zmq4.NewDealer(ctx)
socket.Dial("tcp://server1:5555")
socket.Dial("tcp://server2:5555")
socket.Dial("tcp://server3:5555")

// Binding with wildcard
socket := zmq4.NewRouter(ctx)  
socket.Listen("tcp://*:5555")        // All interfaces
socket.Listen("ipc://local.sock")     // Unix domain socket
```

## 🤝 Contributing

We welcome contributions! Please see our contributing guidelines:

1. **Fork** the repository
2. **Create** a feature branch (`git checkout -b feature/amazing-feature`)
3. **Write** tests for new functionality
4. **Ensure** all tests pass (`make test`)
5. **Commit** changes (`git commit -m 'Add amazing feature'`)
6. **Push** to branch (`git push origin feature/amazing-feature`)  
7. **Open** a Pull Request

### Code Quality Standards
- All code must be tested with black-box testing approach
- Follow Go conventions and best practices
- Add comprehensive documentation for public APIs
- Maintain backward compatibility

## 📄 License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- **ØMQ Community**: For the excellent messaging patterns and protocols
- **Go Community**: For the powerful language and ecosystem
- **Contributors**: Thank you for making this project better

## 📖 Further Reading

- [ØMQ Guide](http://zguide.zeromq.org/) - Comprehensive ØMQ patterns guide
- [CURVE Security RFC](https://rfc.zeromq.org/spec/25/) - CURVE security specification  
- [Majordomo Pattern RFC](https://rfc.zeromq.org/spec/7/) - Service-oriented messaging
- [Zyre Protocol RFC](https://rfc.zeromq.org/spec/36/) - Proximity networking
- [GoDoc Documentation](https://godoc.org/github.com/destiny/zmq4/v25) - Complete API reference

---

**ZMQ4** - Bringing the power of ØMQ to the Go ecosystem with reliability, security, and ease of use.