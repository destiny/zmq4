// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package zyre implements the ZeroMQ Realtime Exchange Protocol (ZRE) as specified by RFC 36.
// ZRE provides proximity-based peer-to-peer communication for local area networks.
// 
// For more information, see https://rfc.zeromq.org/spec/36/
package zyre

import (
	"time"
)

// Protocol constants as per RFC 36/ZRE
const (
	// Protocol version
	ProtocolVersion = 2
	
	// Protocol signature - every ZRE message starts with these bytes
	ProtocolSignature1 = 0xAA
	ProtocolSignature2 = 0xA1
	
	// UDP beacon constants
	BeaconVersion = 1
	BeaconPrefix  = "ZRE"
	BeaconPort    = 5670
	BeaconSize    = 22 // 3 bytes prefix + 1 byte version + 16 bytes UUID + 2 bytes port
	
	// Node UUID size
	UUIDSize = 16
	
	// Default timeouts and intervals
	DefaultBeaconInterval = 1000 * time.Millisecond // Beacon broadcast interval
	DefaultPeerExpiration = 5000 * time.Millisecond // When to consider peer expired
	DefaultHeartbeatInterval = 1000 * time.Millisecond // Heartbeat ping interval
	DefaultConnectionTimeout = 1000 * time.Millisecond // TCP connection timeout
	
	// Maximum sizes
	MaxMessageSize = 255 * 1024 // Maximum ZRE message size
	MaxGroupName   = 255        // Maximum group name length
	MaxNodeName    = 255        // Maximum node name length
)

// ZRE message types as per RFC 36
const (
	MessageTypeHello   = 1 // Initial peer connection
	MessageTypeWhisper = 2 // Unicast message to specific peer
	MessageTypeShout   = 3 // Multicast message to group
	MessageTypeJoin    = 4 // Enter peer group
	MessageTypeLeave   = 5 // Exit peer group
	MessageTypePing    = 6 // Check peer connectivity
	MessageTypePingOK  = 7 // Respond to connectivity check
)

// Node states for state machine
const (
	NodeStateStopped = iota
	NodeStateStarting
	NodeStateRunning
	NodeStateStopping
)

// Event types for the event system
const (
	EventTypeEnter = "ENTER" // New peer has joined the network
	EventTypeExit  = "EXIT"  // Peer has left the network
	EventTypeJoin  = "JOIN"  // Peer has joined a group
	EventTypeLeave = "LEAVE" // Peer has left a group
	EventTypeWhisper = "WHISPER" // Received a direct message
	EventTypeShout = "SHOUT" // Received a group message
)

// Peer status constants
const (
	PeerStatusAlive = iota
	PeerStatusExpired
	PeerStatusDisconnected
)

// Message represents a ZRE protocol message
type Message struct {
	Signature  [2]byte   // Protocol signature
	Version    byte      // Protocol version
	Type       byte      // Message type
	Sequence   uint16    // Message sequence number
	SenderUUID [16]byte  // Sender's UUID
	Body       []byte    // Message body (varies by type)
}

// HelloMessage represents a HELLO message body
type HelloMessage struct {
	Version    byte              // ZRE version
	Sequence   uint16            // Sequence number
	Endpoint   string            // TCP endpoint for peer communication
	Groups     []string          // Groups this peer belongs to
	Status     byte              // Peer status
	Name       string            // Peer name
	Headers    map[string]string // Additional peer headers
}

// WhisperMessage represents a WHISPER message body
type WhisperMessage struct {
	Content [][]byte // Message frames
}

// ShoutMessage represents a SHOUT message body
type ShoutMessage struct {
	Group   string   // Target group name
	Content [][]byte // Message frames
}

// JoinMessage represents a JOIN message body
type JoinMessage struct {
	Group   string // Group name to join
	Status  byte   // Join status
}

// LeaveMessage represents a LEAVE message body
type LeaveMessage struct {
	Group   string // Group name to leave
	Status  byte   // Leave status
}

// PingMessage represents a PING message body
type PingMessage struct {
	// Empty body - just for connectivity check
}

// PingOKMessage represents a PING-OK message body
type PingOKMessage struct {
	// Empty body - response to PING
}