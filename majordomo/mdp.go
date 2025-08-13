// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package majordomo implements the Majordomo Protocol (MDP) as specified by:
// https://rfc.zeromq.org/spec/7/
package majordomo

import (
	"fmt"
	"time"
)

// Protocol constants as per RFC 7/MDP
const (
	// Client protocol identifier
	ClientProtocol = "MDPC01"
	
	// Worker protocol identifier 
	WorkerProtocol = "MDPW01"
	
	// Default heartbeat values
	DefaultHeartbeatLiveness = 3               // 3-5 is reasonable
	DefaultHeartbeatInterval = 2500 * time.Millisecond // msecs
	DefaultHeartbeatExpiry   = DefaultHeartbeatInterval * DefaultHeartbeatLiveness
)

// Worker commands as per MDP specification
const (
	WorkerReady      = "\001" // READY command
	WorkerRequest    = "\002" // REQUEST command
	WorkerReply      = "\003" // REPLY command  
	WorkerHeartbeat  = "\004" // HEARTBEAT command
	WorkerDisconnect = "\005" // DISCONNECT command
)

// ServiceName represents a MDP service name
type ServiceName string

// String returns the service name as a string
func (s ServiceName) String() string {
	return string(s)
}

// Validate checks if the service name is valid
func (s ServiceName) Validate() error {
	if len(s) == 0 {
		return fmt.Errorf("mdp: empty service name")
	}
	if len(s) > 255 {
		return fmt.Errorf("mdp: service name too long: %d bytes (max 255)", len(s))
	}
	return nil
}

// RequestID represents a unique request identifier
type RequestID []byte

// String returns the request ID as a string
func (r RequestID) String() string {
	return fmt.Sprintf("%x", []byte(r))
}

// ClientID represents a unique client identifier  
type ClientID []byte

// String returns the client ID as a string
func (c ClientID) String() string {
	return fmt.Sprintf("%x", []byte(c))
}

// WorkerID represents a unique worker identifier
type WorkerID []byte

// String returns the worker ID as a string  
func (w WorkerID) String() string {
	return fmt.Sprintf("%x", []byte(w))
}

// Message represents a MDP message
type Message struct {
	// Common fields
	Frames [][]byte
	
	// Client message fields
	Service ServiceName
	Body    []byte
	
	// Worker message fields
	Command    string
	ClientAddr []byte
	
	// Timestamps
	Timestamp time.Time
}

// NewClientRequest creates a new client request message
func NewClientRequest(service ServiceName, body []byte) *Message {
	return &Message{
		Service:   service,
		Body:      body,
		Timestamp: time.Now(),
	}
}

// NewClientReply creates a new client reply message
func NewClientReply(service ServiceName, body []byte) *Message {
	return &Message{
		Service:   service,
		Body:      body,
		Timestamp: time.Now(),
	}
}

// NewWorkerMessage creates a new worker protocol message
func NewWorkerMessage(command string, clientAddr []byte, body []byte) *Message {
	return &Message{
		Command:    command,
		ClientAddr: clientAddr,
		Body:       body,
		Timestamp:  time.Now(),
	}
}

// FormatClientRequest formats a message as a client REQUEST
func (m *Message) FormatClientRequest() [][]byte {
	// CLIENT REQUEST: [empty][protocol][service][body...]
	frames := [][]byte{
		{}, // Empty frame
		[]byte(ClientProtocol),
		[]byte(m.Service),
	}
	
	if len(m.Body) > 0 {
		frames = append(frames, m.Body)
	}
	
	return frames
}

// FormatClientReply formats a message as a client REPLY
func (m *Message) FormatClientReply() [][]byte {
	// CLIENT REPLY: [empty][protocol][service][body...]
	frames := [][]byte{
		{}, // Empty frame
		[]byte(ClientProtocol),
		[]byte(m.Service),
	}
	
	if len(m.Body) > 0 {
		frames = append(frames, m.Body)
	}
	
	return frames
}

// FormatWorkerMessage formats a message as a worker protocol message per RFC 7/MDP
func (m *Message) FormatWorkerMessage() [][]byte {
	// Base frames: [empty][protocol][command]
	frames := [][]byte{
		{}, // Empty frame
		[]byte(WorkerProtocol),
		[]byte(m.Command),
	}
	
	// Handle different worker commands according to RFC 7
	switch m.Command {
	case WorkerReady:
		// READY: [empty][protocol][command][service]
		if len(m.Service) > 0 {
			frames = append(frames, []byte(m.Service))
		}
		
	case WorkerRequest, WorkerReply:
		// REQUEST/REPLY: [empty][protocol][command][client][empty][body...]
		if len(m.ClientAddr) > 0 {
			frames = append(frames, m.ClientAddr)
			frames = append(frames, []byte{}) // Empty frame separator
			
			if len(m.Body) > 0 {
				frames = append(frames, m.Body)
			}
		}
		
	case WorkerHeartbeat, WorkerDisconnect:
		// HEARTBEAT/DISCONNECT: [empty][protocol][command]
		// No additional frames needed
		
	default:
		// For unknown commands, include client_addr if present (backwards compatibility)
		if len(m.ClientAddr) > 0 {
			frames = append(frames, m.ClientAddr)
			frames = append(frames, []byte{}) // Empty frame separator
			
			if len(m.Body) > 0 {
				frames = append(frames, m.Body)
			}
		}
	}
	
	return frames
}

// ParseClientMessage parses frames as a client protocol message
func ParseClientMessage(frames [][]byte) (*Message, error) {
	if len(frames) < 3 {
		return nil, fmt.Errorf("mdp: client message too short: %d frames", len(frames))
	}
	
	// Check empty frame
	if len(frames[0]) != 0 {
		return nil, fmt.Errorf("mdp: client message missing empty frame")
	}
	
	// Check protocol
	if string(frames[1]) != ClientProtocol {
		return nil, fmt.Errorf("mdp: invalid client protocol: %s", string(frames[1]))
	}
	
	service := ServiceName(frames[2])
	if err := service.Validate(); err != nil {
		return nil, fmt.Errorf("mdp: %w", err)
	}
	
	msg := &Message{
		Frames:    frames,
		Service:   service,
		Timestamp: time.Now(),
	}
	
	// Extract body if present
	if len(frames) > 3 {
		msg.Body = frames[3]
	}
	
	return msg, nil
}

// ParseWorkerMessage parses frames as a worker protocol message
func ParseWorkerMessage(frames [][]byte) (*Message, error) {
	if len(frames) < 3 {
		return nil, fmt.Errorf("mdp: worker message too short: %d frames", len(frames))
	}
	
	// Check empty frame
	if len(frames[0]) != 0 {
		return nil, fmt.Errorf("mdp: worker message missing empty frame")
	}
	
	// Check protocol
	if string(frames[1]) != WorkerProtocol {
		return nil, fmt.Errorf("mdp: invalid worker protocol: %s", string(frames[1]))
	}
	
	command := string(frames[2])
	
	msg := &Message{
		Frames:    frames,
		Command:   command,
		Timestamp: time.Now(),
	}
	
	// Parse command-specific fields
	switch command {
	case WorkerReady:
		if len(frames) < 4 {
			return nil, fmt.Errorf("mdp: READY message missing service name")
		}
		msg.Service = ServiceName(frames[3])
		if err := msg.Service.Validate(); err != nil {
			return nil, fmt.Errorf("mdp: %w", err)
		}
		
	case WorkerRequest, WorkerReply:
		if len(frames) < 5 {
			return nil, fmt.Errorf("mdp: %s message too short", command)
		}
		msg.ClientAddr = frames[3]
		// frames[4] should be empty separator
		if len(frames[4]) != 0 {
			return nil, fmt.Errorf("mdp: %s message missing empty separator", command)
		}
		
		// Extract body if present
		if len(frames) > 5 {
			msg.Body = frames[5]
		}
		
	case WorkerHeartbeat, WorkerDisconnect:
		// No additional fields required
		
	default:
		return nil, fmt.Errorf("mdp: unknown worker command: %s", command)
	}
	
	return msg, nil
}

// IsExpired checks if the message has expired based on the given TTL
func (m *Message) IsExpired(ttl time.Duration) bool {
	return time.Since(m.Timestamp) > ttl
}

// Age returns the age of the message
func (m *Message) Age() time.Duration {
	return time.Since(m.Timestamp)
}