# ZMQ4 Protocol Implementations

This document provides detailed information about the messaging patterns and high-level protocols implemented in ZMQ4.

## Core ØMQ Socket Types

### Request-Reply Pattern
- **REQ/REP**: Synchronous request-reply for client-server architectures
- **DEALER/ROUTER**: Asynchronous request-reply with routing capabilities

### Publish-Subscribe Pattern
- **PUB/SUB**: One-to-many broadcasting with topic filtering
- **XPUB/XSUB**: Extended pub/sub with subscription forwarding

### Pipeline Pattern
- **PUSH/PULL**: Load-balanced work distribution

### Exclusive Pair
- **PAIR**: One-to-one bidirectional communication

## High-Level Protocols

### Zyre Protocol (RFC 36)
Proximity-based peer-to-peer networking with automatic discovery.

**Features:**
- Automatic peer discovery using UDP beacons
- Group-based messaging
- Event-driven architecture
- Zero-configuration networking

**Use Cases:**
- Local network chat applications
- IoT device coordination
- Service discovery in microservices

### Dafka Protocol
Decentralized streaming with message persistence and ordering.

**Features:**
- Credit-based flow control
- Message persistence
- Publisher/consumer model
- Distributed architecture

**Use Cases:**
- Event streaming platforms
- Message queuing systems
- Real-time data pipelines

### Malamute Protocol
Enterprise messaging broker with streams and services.

**Features:**
- Stream-based messaging
- Service pattern support
- Request-reply semantics
- Connection management

**Use Cases:**
- Enterprise service buses
- Microservice communication
- Message routing systems

### Majordomo Pattern (MDP/RFC 7)
Service-oriented architecture with reliable request-reply.

**Features:**
- Service discovery
- Worker registration
- Heartbeat monitoring
- Load balancing

**Use Cases:**
- Distributed computing
- Microservice orchestration
- API gateways

## Security Mechanisms

### CURVE Security (RFC 25)
Elliptic curve cryptography for secure communication.

**Features:**
- Perfect forward secrecy
- Identity verification
- Anti-replay protection
- Zero-configuration key exchange

### PLAIN Authentication (RFC 27)
Username/password authentication mechanism.

### NULL Security
No authentication for development and testing environments.

## Message Framing

All protocols use ØMQ's multipart message framing:
- Frame 0: Protocol identifier/routing
- Frame 1+: Protocol-specific content

## Implementation Notes

- All protocols are implemented as pure Go packages
- Thread-safe concurrent operations
- Comprehensive error handling
- Memory-efficient design
- Cross-platform compatibility