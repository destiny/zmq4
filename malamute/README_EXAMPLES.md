# Malamute Enterprise Messaging Examples

This directory contains comprehensive examples demonstrating the Malamute enterprise messaging broker implementation. These examples showcase real-world usage patterns for enterprise messaging scenarios.

## Overview

Malamute provides three core messaging patterns:

1. **PUB/SUB Streams** - Real-time data distribution with subject-based routing
2. **Mailboxes** - Direct peer-to-peer messaging with persistence  
3. **Services** - Request-reply patterns with load balancing

All patterns include credit-based flow control for backpressure management.

## Examples

### 1. Broker (`example_broker.go`)

The enterprise messaging broker that handles all client connections and message routing.

**Features:**
- Channel-based concurrent message processing
- Credit-based flow control management
- Real-time statistics reporting
- Graceful shutdown handling
- Support for multiple security mechanisms

**Usage:**
```bash
cd /Users/destiny/Documents/Projects/zmq4/malamute
go run example_broker.go
```

**Expected Output:**
```
Starting Malamute Enterprise Messaging Broker
=============================================
Broker started successfully on tcp://*:9999

=== Broker Statistics ===
Active Connections: 3
Messages Received:  1547
Messages Sent:      1392
Active Streams:     2
Active Mailboxes:   4
Active Services:    2
```

### 2. Publisher (`example_publisher.go`)

Demonstrates real-time stream publishing for financial market data.

**Features:**
- Publishes stock prices to `market.stream`
- Publishes financial news to `news.stream`  
- Automatic credit management
- Message attributes for metadata
- Graceful error handling

**Usage:**
```bash
go run example_publisher.go
```

**Expected Output:**
```
Malamute Stream Publisher Example
================================
Connected to broker at tcp://localhost:9999
Published AAPL: $100.50 (change: 0.5)
Published GOOGL: $99.70 (change: -0.8)
Published news: Market opens higher on positive economic data
```

### 3. Subscriber (`example_subscriber.go`)

Demonstrates stream subscription and message processing.

**Features:**
- Subscribes to market data streams with pattern matching
- Processes financial news updates
- Tracks portfolio data
- Automatic credit monitoring
- Real-time message statistics

**Usage:**
```bash
go run example_subscriber.go
```

**Expected Output:**
```
Malamute Stream Subscriber Example
=================================
Subscriber started. Listening for messages on:
  - market.stream (pattern: market.stocks.*)
  - news.stream (pattern: news.financial)

--- Message #1 ---
Stream: market.stream
Subject: market.stocks.AAPL
Content: {"symbol":"AAPL","price":100.50,"change":0.5}
```

### 4. Service Worker (`example_worker.go`)

Demonstrates service implementation for request-reply patterns.

**Features:**
- Math service (add, subtract, multiply, divide)
- String service (upper, lower, length, reverse)  
- JSON and simple text request formats
- Error handling and validation
- Load balancing across multiple workers

**Usage:**
```bash
go run example_worker.go
```

**Expected Output:**
```
Malamute Service Worker Example
==============================
Service workers started and ready to process requests:
  📊 math.service (pattern: math.*)
  📝 string.service (pattern: string.*)

🧮 Math Request #1
Method: math.add
✅ Sent math reply for tracker: req-123
```

### 5. Service Client (`example_client.go`)

Demonstrates service requests and mailbox messaging.

**Features:**
- Makes various service requests to math and string services
- Sends mailbox messages to different addresses
- Handles service responses and errors
- Credit management and monitoring
- Statistics reporting

**Usage:**
```bash
go run example_client.go
```

**Expected Output:**
```
Malamute Service Client & Mailbox Example
========================================
Service client and mailbox started:
  🔧 Making service requests
  📧 Sending mailbox messages
  
🧮 Sent math request: add 11 + 5 (tracker: math-add-1)
📝 Sent string request: uppercase 'hello world 3' (tracker: string-upper-3)
📧 Sent mailbox message to admin.inbox (tracker: msg-admin.inbox-1)
```

## Running the Complete Example

To see the full system in action:

1. **Start the broker:**
   ```bash
   go run example_broker.go
   ```

2. **In separate terminals, start the workers:**
   ```bash
   go run example_worker.go
   ```

3. **Start the publisher:**
   ```bash
   go run example_publisher.go
   ```

4. **Start the subscriber:**
   ```bash
   go run example_subscriber.go
   ```

5. **Start the service client:**
   ```bash
   go run example_client.go
   ```

You'll see real-time enterprise messaging across all patterns:
- Stream publishing and subscription
- Service requests and replies  
- Mailbox message delivery
- Credit-based flow control
- Load balancing across workers

## Architecture Highlights

### Channel-Based Concurrency
All components use Go channels for thread-safe communication instead of traditional mutex-based synchronization:

```go
// Broker uses channels for message routing
messages         chan *ClientMessage   // Incoming client messages
responses        chan *ClientResponse  // Outgoing responses
commands         chan brokerCmd        // Control commands
```

### Credit-Based Flow Control
Prevents message flooding and provides backpressure:

```go
// Clients request credit before sending messages
response, err := creditController.RequestCredit(clientID, amount, priority)
if response.Granted > 0 {
    // Can send messages up to granted amount
}
```

### Security Integration
Supports multiple ZMQ security mechanisms:

```go
// CURVE encryption
security := malamute.NewCurveSecurity(serverKey, publicKey, secretKey)
secureBroker, err := malamute.NewSecureBroker(config, security)

// PLAIN authentication  
security := malamute.NewPlainSecurity(username, password)
secureClient, err := malamute.NewSecureClient(config, security)
```

## Protocol Compliance

The implementation follows the Malamute protocol specification with:
- Binary message framing compatible with ZeroMQ
- Standard message types and routing
- Credit-based flow control as per specification
- Security mechanism integration

## Production Considerations

For production deployment:

1. **Configure appropriate timeouts and limits**
2. **Enable persistence for mailboxes and streams**
3. **Set up proper security (CURVE recommended)**
4. **Monitor broker statistics and client connections**
5. **Implement proper error handling and retry logic**
6. **Consider clustering for high availability**

These examples provide a solid foundation for building enterprise messaging solutions with Malamute.