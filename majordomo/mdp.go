// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package majordomo implements the Majordomo Protocol (MDP) as specified by:
// https://rfc.zeromq.org/spec/7/
package majordomo

import (
	"fmt"
	"log"
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
	
	// Routing envelope (added by ROUTER sockets)
	RoutingEnvelope [][]byte
	
	// Client message fields
	Service ServiceName
	Body    []byte
	
	// Worker message fields
	Command    string
	ClientAddr []byte
	
	// Client envelope for broker-worker communication
	ClientEnvelope [][]byte
	
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

// FormatClientRequest formats a message as a client REQUEST per RFC 7/MDP
func (m *Message) FormatClientRequest() [][]byte {
	// CLIENT REQUEST: [empty][protocol]["REQUEST"][service][body...]
	frames := [][]byte{
		{}, // Empty delimiter frame
		[]byte(ClientProtocol),
		[]byte("REQUEST"), // Explicit command
		[]byte(m.Service),
	}
	
	if len(m.Body) > 0 {
		frames = append(frames, m.Body)
	}
	
	return frames
}

// FormatClientReply formats a message as a client REPLY (not typically used by clients)
func (m *Message) FormatClientReply() [][]byte {
	// CLIENT REPLY: [routing_envelope...][empty][protocol]["REPLY"][body...]
	// Note: This format is used by broker when replying to client
	frames := [][]byte{}
	
	// Add routing envelope if present
	if len(m.RoutingEnvelope) > 0 {
		frames = append(frames, m.RoutingEnvelope...)
	}
	
	// Add protocol frames
	frames = append(frames, 
		[]byte{}, // Empty delimiter frame
		[]byte(ClientProtocol),
		[]byte("REPLY"), // Explicit command
		[]byte(m.Service), // Service name
	)
	
	if len(m.Body) > 0 {
		frames = append(frames, m.Body)
	}
	
	return frames
}

// FormatWorkerMessage formats a message as a worker protocol message per RFC 7/MDP
func (m *Message) FormatWorkerMessage() [][]byte {
	// Base frames: [routing_envelope...][empty][protocol][command]
	frames := [][]byte{}
	
	// Add routing envelope if present
	if len(m.RoutingEnvelope) > 0 {
		frames = append(frames, m.RoutingEnvelope...)
	}
	
	// Add delimiter and protocol frames
	frames = append(frames,
		[]byte{}, // Empty delimiter frame
		[]byte(WorkerProtocol),
		[]byte(m.Command),
	)
	
	// Handle different worker commands according to RFC 7
	switch m.Command {
	case WorkerReady:
		// READY: [routing...][empty][protocol][command][service]
		if len(m.Service) > 0 {
			frames = append(frames, []byte(m.Service))
		}
		
	case WorkerRequest, WorkerReply:
		// REQUEST/REPLY: [routing...][empty][protocol][command][client_envelope...][empty][body...]
		// Add client envelope frames
		if len(m.ClientEnvelope) > 0 {
			frames = append(frames, m.ClientEnvelope...)
		} else if len(m.ClientAddr) > 0 {
			// Fallback to single client address for compatibility
			frames = append(frames, m.ClientAddr)
		}
		
		// Add empty separator
		frames = append(frames, []byte{})
		
		// Add body if present
		if len(m.Body) > 0 {
			frames = append(frames, m.Body)
		}
		
	case WorkerHeartbeat, WorkerDisconnect:
		// HEARTBEAT/DISCONNECT: [routing...][empty][protocol][command]
		// No additional frames needed
		
	default:
		// For unknown commands, include client envelope if present (backwards compatibility)
		if len(m.ClientEnvelope) > 0 {
			frames = append(frames, m.ClientEnvelope...)
			frames = append(frames, []byte{}) // Empty separator
			
			if len(m.Body) > 0 {
				frames = append(frames, m.Body)
			}
		} else if len(m.ClientAddr) > 0 {
			frames = append(frames, m.ClientAddr)
			frames = append(frames, []byte{}) // Empty separator
			
			if len(m.Body) > 0 {
				frames = append(frames, m.Body)
			}
		}
	}
	
	return frames
}

// findDelimiterFrame finds the empty delimiter frame that separates routing envelope from protocol
func findDelimiterFrame(frames [][]byte) int {
	for i, frame := range frames {
		if len(frame) == 0 {
			return i
		}
	}
	return -1
}

// ParseClientMessage parses frames as a client protocol message with proper routing envelope handling
func ParseClientMessage(frames [][]byte) (*Message, error) {
	if len(frames) < 2 {
		return nil, fmt.Errorf("mdp: client message too short: %d frames", len(frames))
	}
	
	// Find the delimiter frame that separates routing envelope from protocol
	delimiterIdx := findDelimiterFrame(frames)
	
	var routingEnvelope [][]byte
	var protocolFrames [][]byte
	
	if delimiterIdx >= 0 {
		// Found delimiter: extract routing envelope and protocol frames
		routingEnvelope = frames[:delimiterIdx]
		protocolFrames = frames[delimiterIdx+1:] // Skip delimiter frame
	} else {
		// No delimiter found - this might be a direct client message (REQ socket)
		// or a reply from broker to client
		routingEnvelope = nil
		protocolFrames = frames
	}
	
	if len(protocolFrames) < 2 {
		return nil, fmt.Errorf("mdp: client protocol frames too short: %d frames", len(protocolFrames))
	}
	
	// Check protocol header
	if string(protocolFrames[0]) != ClientProtocol {
		return nil, fmt.Errorf("mdp: invalid client protocol: %s", string(protocolFrames[0]))
	}
	
	if len(protocolFrames) < 3 {
		return nil, fmt.Errorf("mdp: client protocol frames missing command or service")
	}
	
	// Extract command (should be REQUEST or REPLY)
	command := string(protocolFrames[1])
	
	var service ServiceName
	var bodyIdx int
	
	switch command {
	case "REQUEST":
		if len(protocolFrames) < 3 {
			return nil, fmt.Errorf("mdp: REQUEST message missing service name")
		}
		service = ServiceName(protocolFrames[2])
		bodyIdx = 3
		
	case "REPLY":
		// REPLY messages have format: [protocol]["REPLY"][service][body...]
		if len(protocolFrames) < 3 {
			return nil, fmt.Errorf("mdp: REPLY message missing service name")
		}
		service = ServiceName(protocolFrames[2])
		bodyIdx = 3
		
	default:
		// For backward compatibility, treat as old format: [protocol][service][body...]
		service = ServiceName(protocolFrames[1])
		bodyIdx = 2
	}
	
	if len(service) > 0 {
		if err := service.Validate(); err != nil {
			return nil, fmt.Errorf("mdp: %w", err)
		}
	}
	
	msg := &Message{
		Frames:          frames,
		RoutingEnvelope: routingEnvelope,
		Command:         command,
		Service:         service,
		Timestamp:       time.Now(),
	}
	
	// Extract body if present
	if len(protocolFrames) > bodyIdx {
		msg.Body = protocolFrames[bodyIdx]
	}
	
	return msg, nil
}

// ParseWorkerMessage parses frames as a worker protocol message with proper routing envelope handling
func ParseWorkerMessage(frames [][]byte) (*Message, error) {
	log.Printf("ParseWorkerMessage: starting with %d frames", len(frames))
	for i, frame := range frames {
		if len(frame) > 0 {
			log.Printf("  input_frame[%d]: %q (len=%d)", i, string(frame), len(frame))
		} else {
			log.Printf("  input_frame[%d]: <empty> (len=%d)", i, len(frame))
		}
	}
	
	if len(frames) < 3 {
		log.Printf("ParseWorkerMessage: FAILED - too short: %d frames", len(frames))
		return nil, fmt.Errorf("mdp: worker message too short: %d frames", len(frames))
	}
	
	// Find the delimiter frame that separates routing envelope from protocol
	delimiterIdx := findDelimiterFrame(frames)
	log.Printf("ParseWorkerMessage: delimiter found at index %d", delimiterIdx)
	
	var routingEnvelope [][]byte
	var protocolFrames [][]byte
	
	if delimiterIdx >= 0 {
		// Found delimiter: extract routing envelope and protocol frames
		routingEnvelope = frames[:delimiterIdx]
		protocolFrames = frames[delimiterIdx+1:] // Skip delimiter frame
		log.Printf("ParseWorkerMessage: routing envelope has %d frames, protocol has %d frames", len(routingEnvelope), len(protocolFrames))
	} else {
		log.Printf("ParseWorkerMessage: FAILED - missing empty separator")
		return nil, fmt.Errorf("mdp: worker message missing empty separator")
	}
	
	if len(protocolFrames) < 2 {
		log.Printf("ParseWorkerMessage: FAILED - protocol frames too short: %d frames", len(protocolFrames))
		return nil, fmt.Errorf("mdp: worker protocol frames too short: %d frames", len(protocolFrames))
	}
	
	// Check protocol header
	log.Printf("ParseWorkerMessage: checking protocol header: %q vs expected %q", string(protocolFrames[0]), WorkerProtocol)
	if string(protocolFrames[0]) != WorkerProtocol {
		log.Printf("ParseWorkerMessage: FAILED - invalid protocol header: %s", string(protocolFrames[0]))
		return nil, fmt.Errorf("mdp: invalid worker protocol: %s", string(protocolFrames[0]))
	}
	
	// Extract command
	command := string(protocolFrames[1])
	log.Printf("ParseWorkerMessage: extracted command: %q (bytes=%v)", command, []byte(command))
	
	msg := &Message{
		Frames:          frames,
		RoutingEnvelope: routingEnvelope,
		Command:         command,
		Timestamp:       time.Now(),
	}
	
	// Parse command-specific fields
	switch command {
	case WorkerReady:
		if len(protocolFrames) < 3 {
			return nil, fmt.Errorf("mdp: READY message missing service name")
		}
		msg.Service = ServiceName(protocolFrames[2])
		if err := msg.Service.Validate(); err != nil {
			return nil, fmt.Errorf("mdp: %w", err)
		}
		
	case WorkerRequest, WorkerReply:
		log.Printf("ParseWorkerMessage: processing %s command", command)
		if len(protocolFrames) < 3 {
			log.Printf("ParseWorkerMessage: FAILED - %s message too short: %d frames", command, len(protocolFrames))
			return nil, fmt.Errorf("mdp: %s message too short", command)
		}
		
		// For REQUEST/REPLY, we need to parse the client envelope
		// Format: [protocol][command][client_envelope...][empty_separator][body]
		log.Printf("ParseWorkerMessage: looking for empty separator in protocol frames:")
		for i, frame := range protocolFrames {
			if len(frame) > 0 {
				log.Printf("  protocol_frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  protocol_frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
		
		// Find the empty separator that marks end of client envelope
		separatorIdx := -1
		for i := 2; i < len(protocolFrames); i++ {
			if len(protocolFrames[i]) == 0 {
				separatorIdx = i
				log.Printf("ParseWorkerMessage: found separator at protocol index %d", i)
				break
			}
		}
		
		if separatorIdx == -1 {
			log.Printf("ParseWorkerMessage: FAILED - %s message missing empty separator", command)
			return nil, fmt.Errorf("mdp: %s message missing empty separator", command)
		}
		
		// Extract client envelope (frames between command and separator)
		if separatorIdx > 2 {
			msg.ClientEnvelope = protocolFrames[2:separatorIdx]
			log.Printf("ParseWorkerMessage: extracted client envelope with %d frames", len(msg.ClientEnvelope))
			// For compatibility, set ClientAddr to first frame of envelope
			if len(msg.ClientEnvelope) > 0 {
				msg.ClientAddr = msg.ClientEnvelope[0]
				log.Printf("ParseWorkerMessage: client addr: %q", string(msg.ClientAddr))
			}
		}
		
		// Extract body if present (after separator)
		if len(protocolFrames) > separatorIdx+1 {
			msg.Body = protocolFrames[separatorIdx+1]
			log.Printf("ParseWorkerMessage: extracted body: %q (len=%d)", string(msg.Body), len(msg.Body))
		}
		
	case WorkerHeartbeat, WorkerDisconnect:
		// No additional fields required
		
	default:
		log.Printf("ParseWorkerMessage: FAILED - unknown command: %s", command)
		return nil, fmt.Errorf("mdp: unknown worker command: %s", command)
	}
	
	log.Printf("ParseWorkerMessage: SUCCESS - parsed %s command", command)
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