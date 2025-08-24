// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package malamute implements the Malamute enterprise messaging broker protocol.
// Malamute provides a comprehensive messaging solution with streams, mailboxes,
// services, and credit-based flow control for enterprise applications.
//
// The broker supports three main communication patterns:
// - PUB/SUB streams for real-time data distribution
// - Mailboxes for direct peer-to-peer messaging
// - Services for request-reply and load balancing
package malamute

import (
	"time"
)

// Protocol constants for Malamute
const (
	// Protocol version
	ProtocolVersion = 1
	
	// Default ports and addresses
	DefaultBrokerPort = 9999
	DefaultEndpoint   = "tcp://localhost:9999"
	
	// Credit-Based Flow Control constants
	DefaultCreditWindow = 1000    // Default credit window size
	MinCreditWindow     = 10      // Minimum credit window
	MaxCreditWindow     = 100000  // Maximum credit window
	
	// Message size limits
	MaxMessageSize   = 16 * 1024 * 1024 // 16MB maximum message size
	MaxStreamName    = 255              // Maximum stream name length
	MaxMailboxName   = 255              // Maximum mailbox name length
	MaxServiceName   = 255              // Maximum service name length
	MaxClientID      = 255              // Maximum client ID length
	
	// Timeouts and intervals
	DefaultHeartbeatInterval = 5000 * time.Millisecond  // Heartbeat interval
	DefaultClientTimeout     = 60000 * time.Millisecond // Client timeout
	DefaultServiceTimeout    = 30000 * time.Millisecond // Service timeout
	DefaultRetryInterval     = 1000 * time.Millisecond  // Retry interval
	
	// Queue limits
	MaxStreamBacklog  = 100000 // Maximum messages in stream backlog
	MaxMailboxBacklog = 10000  // Maximum messages in mailbox
	MaxServiceBacklog = 10000  // Maximum messages in service queue
)

// Message types for Malamute protocol
const (
	// Client connection messages
	MessageTypeConnectionOpen  = 1 // Client opens connection to broker
	MessageTypeConnectionPing  = 2 // Client sends heartbeat
	MessageTypeConnectionPong  = 3 // Broker responds to heartbeat
	MessageTypeConnectionClose = 4 // Client closes connection
	
	// Stream messages
	MessageTypeStreamWrite   = 10 // Write message to stream
	MessageTypeStreamRead    = 11 // Read messages from stream
	MessageTypeStreamSend    = 12 // Send message on stream (broker to client)
	MessageTypeStreamCancel  = 13 // Cancel stream subscription
	
	// Mailbox messages
	MessageTypeMailboxSend    = 20 // Send message to mailbox
	MessageTypeMailboxReceive = 21 // Receive message from mailbox
	MessageTypeMailboxDeliver = 22 // Deliver message to mailbox (broker to client)
	
	// Service messages
	MessageTypeServiceOffer   = 30 // Offer a service
	MessageTypeServiceWithdraw = 31 // Withdraw service offer
	MessageTypeServiceRequest = 32 // Send service request
	MessageTypeServiceReply   = 33 // Send service reply
	MessageTypeServiceDeliver = 34 // Deliver service request (broker to worker)
	
	// Credit messages
	MessageTypeCreditRequest = 40 // Request credit from broker
	MessageTypeCreditConfirm = 41 // Broker confirms credit
	
	// Error and status messages
	MessageTypeError = 99 // Error response
)

// Client types
const (
	ClientTypeGeneral = iota // General purpose client
	ClientTypePublisher      // Stream publisher
	ClientTypeSubscriber     // Stream subscriber
	ClientTypeWorker         // Service worker
	ClientTypeRequester      // Service requester
)

// Stream message attributes
const (
	StreamAttrPersistent = "persistent" // Stream is persistent
	StreamAttrReplay     = "replay"     // Allow replay of messages
	StreamAttrTimeToLive = "ttl"        // Message time-to-live
	StreamAttrPriority   = "priority"   // Message priority
)

// Service message attributes
const (
	ServiceAttrTimeout    = "timeout"    // Service timeout
	ServiceAttrRetry      = "retry"      // Retry count
	ServiceAttrPriority   = "priority"   // Request priority
	ServiceAttrCorrelation = "correlation" // Correlation ID
)

// Error codes
const (
	ErrorCodeSuccess           = 0   // No error
	ErrorCodeInvalidMessage    = 1   // Invalid message format
	ErrorCodeAccessDenied      = 2   // Access denied
	ErrorCodeResourceNotFound  = 3   // Resource not found
	ErrorCodeResourceConflict  = 4   // Resource conflict
	ErrorCodeInternalError     = 5   // Internal broker error
	ErrorCodeNotImplemented    = 6   // Feature not implemented
	ErrorCodeInvalidArgument   = 7   // Invalid argument
	ErrorCodeQuotaExceeded     = 8   // Quota exceeded
	ErrorCodeServiceTimeout    = 9   // Service timeout
	ErrorCodeConnectionClosed  = 10  // Connection closed
)

// Message represents a Malamute protocol message
type Message struct {
	Type       byte              // Message type
	Version    byte              // Protocol version
	Sequence   uint32            // Message sequence number
	ClientID   string            // Client identifier
	Attributes map[string]string // Message attributes
	Body       []byte            // Message body
}

// ConnectionOpenMessage represents a connection open request
type ConnectionOpenMessage struct {
	ClientID   string            // Client identifier
	ClientType int               // Client type
	Attributes map[string]string // Client attributes
}

// ConnectionPingMessage represents a heartbeat ping
type ConnectionPingMessage struct {
	Timestamp int64 // Ping timestamp
}

// ConnectionPongMessage represents a heartbeat pong
type ConnectionPongMessage struct {
	Timestamp int64 // Original ping timestamp
}

// StreamWriteMessage represents a stream write request
type StreamWriteMessage struct {
	Stream     string            // Stream name
	Subject    string            // Message subject
	Attributes map[string]string // Message attributes
	Body       []byte            // Message body
}

// StreamReadMessage represents a stream read request
type StreamReadMessage struct {
	Stream     string            // Stream name
	Pattern    string            // Subject pattern to match
	Attributes map[string]string // Read attributes
}

// StreamSendMessage represents a stream message delivery
type StreamSendMessage struct {
	Stream     string            // Stream name
	Subject    string            // Message subject
	Sender     string            // Message sender
	Attributes map[string]string // Message attributes
	Body       []byte            // Message body
	Timestamp  int64             // Message timestamp
}

// MailboxSendMessage represents a mailbox send request
type MailboxSendMessage struct {
	Address    string            // Mailbox address
	Subject    string            // Message subject
	Tracker    string            // Message tracker (optional)
	Timeout    int64             // Message timeout
	Attributes map[string]string // Message attributes
	Body       []byte            // Message body
}

// MailboxDeliverMessage represents a mailbox message delivery
type MailboxDeliverMessage struct {
	Sender     string            // Message sender
	Subject    string            // Message subject
	Tracker    string            // Message tracker
	Attributes map[string]string // Message attributes
	Body       []byte            // Message body
	Timestamp  int64             // Message timestamp
}

// ServiceOfferMessage represents a service offer
type ServiceOfferMessage struct {
	Service    string            // Service name
	Pattern    string            // Request pattern to handle
	Attributes map[string]string // Service attributes
}

// ServiceRequestMessage represents a service request
type ServiceRequestMessage struct {
	Service    string            // Service name
	Method     string            // Service method
	Tracker    string            // Request tracker
	Timeout    int64             // Request timeout
	Attributes map[string]string // Request attributes
	Body       []byte            // Request body
}

// ServiceReplyMessage represents a service reply
type ServiceReplyMessage struct {
	Tracker    string            // Request tracker
	Status     int               // Reply status code
	Attributes map[string]string // Reply attributes
	Body       []byte            // Reply body
}

// ServiceDeliverMessage represents a service request delivery to worker
type ServiceDeliverMessage struct {
	Service    string            // Service name
	Method     string            // Service method
	Sender     string            // Request sender
	Tracker    string            // Request tracker
	Attributes map[string]string // Request attributes
	Body       []byte            // Request body
	Timestamp  int64             // Request timestamp
}

// CreditRequestMessage represents a credit request
type CreditRequestMessage struct {
	Credit int32 // Requested credit amount
}

// CreditConfirmMessage represents a credit confirmation
type CreditConfirmMessage struct {
	Credit int32 // Confirmed credit amount
}

// ErrorMessage represents an error response
type ErrorMessage struct {
	Code       int               // Error code
	Reason     string            // Error reason
	Attributes map[string]string // Error attributes
}

// Stream represents a message stream
type Stream struct {
	Name         string            // Stream name
	Messages     []*StreamMessage  // Stored messages
	Subscribers  map[string]*StreamSubscription // Active subscriptions
	Attributes   map[string]string // Stream attributes
	CreatedAt    time.Time         // Creation timestamp
	LastActivity time.Time         // Last activity timestamp
	MessageCount uint64            // Total message count
	BytesStored  uint64            // Total bytes stored
}

// StreamMessage represents a message in a stream
type StreamMessage struct {
	ID         uint64            // Message ID
	Subject    string            // Message subject
	Sender     string            // Message sender
	Attributes map[string]string // Message attributes
	Body       []byte            // Message body
	Timestamp  time.Time         // Message timestamp
	TTL        time.Duration     // Time to live
	Priority   int               // Message priority
}

// StreamSubscription represents a stream subscription
type StreamSubscription struct {
	ClientID   string            // Subscriber client ID
	Pattern    string            // Subject pattern
	Credit     int32             // Available credit
	Attributes map[string]string // Subscription attributes
	CreatedAt  time.Time         // Subscription timestamp
	LastRead   uint64            // Last read message ID
}

// Mailbox represents a client mailbox
type Mailbox struct {
	Address      string            // Mailbox address
	Messages     []*MailboxMessage // Queued messages
	Owner        string            // Mailbox owner
	Attributes   map[string]string // Mailbox attributes
	CreatedAt    time.Time         // Creation timestamp
	LastActivity time.Time         // Last activity timestamp
	MessageCount uint64            // Total message count
	BytesStored  uint64            // Total bytes stored
}

// MailboxMessage represents a message in a mailbox
type MailboxMessage struct {
	ID         uint64            // Message ID
	Subject    string            // Message subject
	Sender     string            // Message sender
	Tracker    string            // Message tracker
	Attributes map[string]string // Message attributes
	Body       []byte            // Message body
	Timestamp  time.Time         // Message timestamp
	Timeout    time.Time         // Message timeout
	Retries    int               // Retry count
}

// Service represents a service definition
type Service struct {
	Name         string              // Service name
	Workers      map[string]*ServiceWorker // Available workers
	Queue        []*ServiceRequest   // Pending requests
	Attributes   map[string]string   // Service attributes
	CreatedAt    time.Time           // Creation timestamp
	LastActivity time.Time           // Last activity timestamp
	RequestCount uint64              // Total request count
	ReplyCount   uint64              // Total reply count
}

// ServiceWorker represents a service worker
type ServiceWorker struct {
	ClientID     string            // Worker client ID
	Pattern      string            // Handled request pattern
	Credit       int32             // Available credit
	Attributes   map[string]string // Worker attributes
	RegisteredAt time.Time         // Registration timestamp
	LastRequest  time.Time         // Last request timestamp
	RequestCount uint64            // Handled request count
}

// ServiceRequest represents a service request
type ServiceRequest struct {
	ID         uint64            // Request ID
	Service    string            // Service name
	Method     string            // Service method
	Sender     string            // Request sender
	Tracker    string            // Request tracker
	Attributes map[string]string // Request attributes
	Body       []byte            // Request body
	Timestamp  time.Time         // Request timestamp
	Timeout    time.Time         // Request timeout
	Retries    int               // Retry count
}

// Client represents a connected client
type Client struct {
	ID           string            // Client identifier
	Type         int               // Client type
	Address      string            // Client address
	Connection   interface{}       // Network connection
	Credit       int32             // Available credit
	Attributes   map[string]string // Client attributes
	ConnectedAt  time.Time         // Connection timestamp
	LastActivity time.Time         // Last activity timestamp
	MessagesSent uint64            // Messages sent
	MessagesRecv uint64            // Messages received
	BytesSent    uint64            // Bytes sent
	BytesRecv    uint64            // Bytes received
}

// CreditManager manages credit-based flow control
type CreditManager struct {
	ClientCredit map[string]int32  // Credit per client
	WindowSize   int32             // Credit window size
	LowWatermark int32             // Low watermark for credit renewal
	HighWatermark int32            // High watermark for credit limiting
}

// BrokerConfig holds broker configuration
type BrokerConfig struct {
	Name               string        // Broker name
	Endpoint           string        // Broker endpoint
	Verbose            bool          // Verbose logging
	CreditWindow       int32         // Credit window size
	HeartbeatInterval  time.Duration // Heartbeat interval
	ClientTimeout      time.Duration // Client timeout
	ServiceTimeout     time.Duration // Service timeout
	MaxConnections     int           // Maximum concurrent connections
	MaxStreams         int           // Maximum number of streams
	MaxMailboxes       int           // Maximum number of mailboxes
	MaxServices        int           // Maximum number of services
	StreamRetention    time.Duration // Stream message retention
	MailboxRetention   time.Duration // Mailbox message retention
	EnablePersistence  bool          // Enable message persistence
	DataDirectory      string        // Data directory for persistence
}

// ClientConfig holds client configuration
type ClientConfig struct {
	ClientID          string        // Client identifier
	BrokerEndpoint    string        // Broker endpoint
	HeartbeatInterval time.Duration // Heartbeat interval
	Timeout           time.Duration // Operation timeout
	CreditWindow      int32         // Credit window size
	MaxRetries        int           // Maximum retry attempts
	RetryInterval     time.Duration // Retry interval
	Verbose           bool          // Verbose logging
}

// Statistics holds broker statistics
type Statistics struct {
	// Connection statistics
	TotalConnections   uint64 // Total connections made
	ActiveConnections  uint64 // Current active connections
	ConnectionErrors   uint64 // Connection errors
	
	// Message statistics
	MessagesReceived   uint64 // Total messages received
	MessagesSent       uint64 // Total messages sent
	BytesReceived      uint64 // Total bytes received
	BytesSent          uint64 // Total bytes sent
	MessagesDropped    uint64 // Messages dropped
	
	// Stream statistics
	StreamsCreated     uint64 // Total streams created
	ActiveStreams      uint64 // Current active streams
	StreamMessages     uint64 // Total stream messages
	StreamBytes        uint64 // Total stream bytes
	
	// Mailbox statistics
	MailboxesCreated   uint64 // Total mailboxes created
	ActiveMailboxes    uint64 // Current active mailboxes
	MailboxMessages    uint64 // Total mailbox messages
	MailboxBytes       uint64 // Total mailbox bytes
	
	// Service statistics
	ServicesRegistered uint64 // Total services registered
	ActiveServices     uint64 // Current active services
	ServiceRequests    uint64 // Total service requests
	ServiceReplies     uint64 // Total service replies
	ServiceTimeouts    uint64 // Service timeouts
	
	// Credit statistics
	CreditRequests     uint64 // Credit requests
	CreditConfirms     uint64 // Credit confirmations
	CreditDenials      uint64 // Credit denials
	
	// Error statistics
	ProtocolErrors     uint64 // Protocol errors
	InternalErrors     uint64 // Internal errors
	
	// Timing statistics
	StartTime          time.Time // Broker start time
	LastUpdate         time.Time // Last statistics update
}