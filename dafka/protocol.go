// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dafka implements the DAFKA protocol as specified by RFC 46.
// DAFKA is a decentralized distributed streaming platform that provides
// publish-subscribe messaging with durable record storage.
//
// For more information, see https://rfc.zeromq.org/spec/46/
package dafka

import (
	"time"
)

// Protocol constants as per RFC 46/DAFKA
const (
	// Protocol version
	ProtocolVersion = 1
	
	// Protocol signature - every DAFKA message starts with these bytes
	ProtocolSignature1 = 0xAA
	ProtocolSignature2 = 0xA0
	
	// Node UUID size
	UUIDSize = 16
	
	// Default timeouts and intervals
	DefaultTowerPort      = 5670                          // Default tower port
	DefaultBeaconInterval = 1000 * time.Millisecond      // Beacon broadcast interval
	DefaultHeartbeatInterval = 2000 * time.Millisecond   // Heartbeat interval
	DefaultFetchTimeout   = 5000 * time.Millisecond      // Record fetch timeout
	DefaultStoreTimeout   = 10000 * time.Millisecond     // Store operation timeout
	
	// Limits
	MaxTopicLength     = 255   // Maximum topic name length
	MaxRecordSize      = 1024 * 1024 // Maximum record size (1MB)
	MaxBatchSize       = 1000  // Maximum records per batch
	MaxPartitions      = 65535 // Maximum partitions per topic
)

// DAFKA message types as per RFC 46
const (
	MessageTypeStoreHello    = 1  // Store node announces availability
	MessageTypeConsumerHello = 2  // Consumer announces subscription
	MessageTypeRecord        = 3  // Producer publishes a record
	MessageTypeAck          = 4  // Consumer acknowledges record receipt
	MessageTypeHead         = 5  // Store announces highest offset
	MessageTypeFetch        = 6  // Consumer requests records
	MessageTypeDirectRecord = 7  // Direct record delivery
	MessageTypeGetHeads     = 8  // Request for HEAD messages
	MessageTypeDirectHead   = 9  // Direct HEAD delivery
)

// Node types
const (
	NodeTypeProducer = iota
	NodeTypeConsumer
	NodeTypeStore
	NodeTypeTower
)

// Node states
const (
	NodeStateStopped = iota
	NodeStateStarting
	NodeStateRunning
	NodeStateStopping
)

// Record represents a single data record in a topic partition
type Record struct {
	Topic     string    // Topic name
	Partition string    // Partition identifier (producer UUID)
	Offset    uint64    // Record offset within partition
	Key       []byte    // Record key (optional)
	Value     []byte    // Record value
	Headers   map[string]string // Record headers
	Timestamp time.Time // Record timestamp
}

// Message represents a DAFKA protocol message
type Message struct {
	Signature [2]byte  // Protocol signature
	Version   byte     // Protocol version
	Type      byte     // Message type
	Topic     string   // Topic name
	Body      []byte   // Message body (varies by type)
}

// StoreHelloMessage represents a STORE-HELLO message body
type StoreHelloMessage struct {
	Address string            // Store's network address
	Headers map[string]string // Store headers
}

// ConsumerHelloMessage represents a CONSUMER-HELLO message body
type ConsumerHelloMessage struct {
	Address string            // Consumer's network address
	Headers map[string]string // Consumer headers
}

// RecordMessage represents a RECORD message body
type RecordMessage struct {
	Partition string    // Partition identifier
	Offset    uint64    // Record offset
	Key       []byte    // Record key
	Value     []byte    // Record value
	Headers   map[string]string // Record headers
}

// AckMessage represents an ACK message body
type AckMessage struct {
	Partition string // Partition identifier
	Offset    uint64 // Acknowledged offset
}

// HeadMessage represents a HEAD message body
type HeadMessage struct {
	Partition string // Partition identifier
	Offset    uint64 // Highest offset in partition
}

// FetchMessage represents a FETCH message body
type FetchMessage struct {
	Partition string // Partition identifier
	Offset    uint64 // Starting offset to fetch
	Count     uint32 // Number of records to fetch
}

// DirectRecordMessage represents a DIRECT-RECORD message body
type DirectRecordMessage struct {
	Partition string    // Partition identifier
	Offset    uint64    // Record offset
	Key       []byte    // Record key
	Value     []byte    // Record value
	Headers   map[string]string // Record headers
}

// GetHeadsMessage represents a GET-HEADS message body
type GetHeadsMessage struct {
	// Empty body - requests HEAD messages for all partitions
}

// DirectHeadMessage represents a DIRECT-HEAD message body
type DirectHeadMessage struct {
	Partition string // Partition identifier
	Offset    uint64 // Highest offset in partition
}

// Subscription represents a consumer's subscription to a topic
type Subscription struct {
	Topic       string            // Topic name
	Partitions  map[string]bool   // Subscribed partitions (empty = all)
	Offset      map[string]uint64 // Last consumed offset per partition
	Headers     map[string]string // Subscription headers
	StartTime   time.Time         // When subscription started
}

// PartitionInfo holds information about a topic partition
type PartitionInfo struct {
	Topic     string    // Topic name
	Partition string    // Partition identifier (producer UUID)
	Producer  string    // Producer node UUID
	HeadOffset uint64   // Highest offset in partition
	LastSeen  time.Time // When partition was last seen
	Stores    []string  // Store nodes holding this partition
}

// TopicInfo holds information about a topic
type TopicInfo struct {
	Name       string                    // Topic name
	Partitions map[string]*PartitionInfo // Partition information
	CreatedAt  time.Time                 // When topic was first seen
	UpdatedAt  time.Time                 // When topic was last updated
}

// NodeInfo represents information about a DAFKA node
type NodeInfo struct {
	UUID      string            // Node UUID
	Type      int               // Node type (Producer, Consumer, Store, Tower)
	Address   string            // Network address
	Topics    []string          // Topics this node handles
	Headers   map[string]string // Node headers
	LastSeen  time.Time         // When node was last seen
	Connected bool              // Whether we have a connection
}

// RecordBatch represents a batch of records for efficient processing
type RecordBatch struct {
	Topic     string    // Topic name
	Partition string    // Partition identifier
	Records   []*Record // Records in the batch
	BaseOffset uint64   // Offset of first record
	BatchSize int       // Number of records
	Timestamp time.Time // Batch creation time
}

// ConsumerGroup represents a group of consumers sharing topic consumption
type ConsumerGroup struct {
	ID        string              // Group identifier
	Members   map[string]*NodeInfo // Group members
	Topics    []string            // Subscribed topics
	Offsets   map[string]uint64   // Committed offsets per partition
	Leader    string              // Group leader UUID
	CreatedAt time.Time           // When group was created
	UpdatedAt time.Time           // When group was last updated
}

// ProducerConfig holds configuration for a producer
type ProducerConfig struct {
	NodeUUID  string            // Producer UUID (also partition ID)
	Address   string            // Network address to bind
	TowerAddr string            // Tower address to connect
	Headers   map[string]string // Producer headers
	BatchSize int               // Records per batch
	LingerMs  int               // Batch linger time in milliseconds
}

// ConsumerConfig holds configuration for a consumer
type ConsumerConfig struct {
	NodeUUID    string            // Consumer UUID
	Address     string            // Network address to bind
	TowerAddr   string            // Tower address to connect
	GroupID     string            // Consumer group ID
	Headers     map[string]string // Consumer headers
	FetchSize   int               // Records per fetch request
	AutoCommit  bool              // Whether to auto-commit offsets
	CommitIntervalMs int          // Auto-commit interval in milliseconds
}

// StoreConfig holds configuration for a store
type StoreConfig struct {
	NodeUUID    string            // Store UUID
	Address     string            // Network address to bind
	TowerAddr   string            // Tower address to connect
	DataDir     string            // Directory for persistent storage
	Headers     map[string]string // Store headers
	SyncInterval time.Duration    // How often to sync to disk
	RetentionMs int64             // Record retention time in milliseconds
}

// TowerConfig holds configuration for a tower
type TowerConfig struct {
	Address       string            // Network address to bind
	BeaconPort    uint16            // UDP beacon port
	Headers       map[string]string // Tower headers
	DiscoveryInterval time.Duration // Node discovery interval
}

// Error types for DAFKA operations
const (
	ErrTopicNotFound     = "topic not found"
	ErrPartitionNotFound = "partition not found"
	ErrOffsetOutOfRange  = "offset out of range"
	ErrProducerNotFound  = "producer not found"
	ErrStoreNotAvailable = "store not available"
	ErrInvalidMessage    = "invalid message format"
	ErrConnectionFailed  = "connection failed"
	ErrTimeout           = "operation timeout"
)

// Statistics holds metrics about DAFKA operations
type Statistics struct {
	// Producer statistics
	RecordsProduced uint64 // Total records produced
	BytesProduced   uint64 // Total bytes produced
	ProduceErrors   uint64 // Number of produce errors
	
	// Consumer statistics
	RecordsConsumed uint64 // Total records consumed
	BytesConsumed   uint64 // Total bytes consumed
	ConsumeErrors   uint64 // Number of consume errors
	
	// Store statistics
	RecordsStored uint64 // Total records stored
	BytesStored   uint64 // Total bytes stored
	StoreErrors   uint64 // Number of store errors
	
	// Network statistics
	MessagesIn  uint64 // Messages received
	MessagesOut uint64 // Messages sent
	BytesIn     uint64 // Bytes received
	BytesOut    uint64 // Bytes sent
	
	// Connection statistics
	Connections uint64 // Active connections
	Reconnects  uint64 // Number of reconnections
	
	// Timing statistics
	StartTime   time.Time // When node started
	LastUpdate  time.Time // Last statistics update
}