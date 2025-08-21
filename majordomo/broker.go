// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package majordomo

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	zmq4 "github.com/destiny/zmq4/v25"
)

// Message type constants for frame analysis
const (
	messageTypeUnknown = iota
	messageTypeWorker
	messageTypeClient
)

// BrokerWorker represents a connected worker from the broker's perspective
type BrokerWorker struct {
	ID       WorkerID
	Service  ServiceName
	Identity []byte
	Address  []byte
	Expiry   time.Time
	InstanceID string // Unique instance identifier for debugging
	
	// Statistics
	RequestsProcessed uint64
	LastSeen         time.Time
}

// IsExpired checks if the worker has expired
func (w *BrokerWorker) IsExpired() bool {
	return time.Now().After(w.Expiry)
}

// Service represents a service with its worker queue
type Service struct {
	Name             ServiceName
	Requests         []*PendingRequest
	AvailableWorkers []*BrokerWorker  // Workers ready to accept requests (single source of truth)
	BusyWorkers      []*BrokerWorker  // Workers currently processing requests
	
	// Statistics
	TotalRequests  uint64
	TotalResponses uint64
	ActiveWorkers  int
}

// PendingRequest represents a request waiting for a worker
type PendingRequest struct {
	Client  []byte
	Service ServiceName
	Body    []byte
	Created time.Time
}

// IsExpired checks if the request has expired
func (r *PendingRequest) IsExpired(ttl time.Duration) bool {
	return time.Since(r.Created) > ttl
}

// BrokerOptions configures MDP broker behavior
type BrokerOptions struct {
	HeartbeatLiveness int           // Heartbeat liveness factor
	HeartbeatInterval time.Duration // Heartbeat interval
	RequestTimeout    time.Duration // Request timeout
	Security          zmq4.Security // Security mechanism (nil for no security)
	LogLevel          zmq4.LogLevel // Logging level (replaces LogErrors/LogInfo)
	
	// Deprecated: Use LogLevel instead
	LogErrors         bool          // Whether to log errors
	LogInfo           bool          // Whether to log info messages
}

// DefaultBrokerOptions returns default broker options
func DefaultBrokerOptions() *BrokerOptions {
	return &BrokerOptions{
		HeartbeatLiveness: DefaultHeartbeatLiveness,
		HeartbeatInterval: DefaultHeartbeatInterval,
		RequestTimeout:    30 * time.Second,
		Security:          nil,
		LogLevel:          zmq4.LogLevelWarn, // Default to WARN level
		// Backward compatibility
		LogErrors:         true,
		LogInfo:           false,
	}
}

// Broker implements the MDP broker
type Broker struct {
	// Configuration
	endpoint         string
	heartbeatLiveness int
	heartbeatInterval time.Duration
	heartbeatExpiry   time.Duration
	requestTimeout    time.Duration
	options           *BrokerOptions
	
	// Networking
	socket zmq4.Socket
	ctx    context.Context
	cancel context.CancelFunc
	
	// State management
	services map[ServiceName]*Service
	workers  map[string]*BrokerWorker // Key is worker identity as string
	
	// Statistics
	totalClients  uint64
	totalWorkers  uint64
	totalRequests uint64
	totalReplies  uint64
	
	// Logging
	logger     *zmq4.Logger
	
	// Synchronization
	mu         sync.RWMutex
	running    bool
	stopCh     chan struct{}
	workerCh   chan *Message
	clientCh   chan *Message
	heartbeatCh chan struct{}
}

// NewBroker creates a new MDP broker
func NewBroker(endpoint string, options *BrokerOptions) *Broker {
	if options == nil {
		options = DefaultBrokerOptions()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Initialize logger based on options
	var logger *zmq4.Logger
	if options.LogLevel != 0 {
		// Use new LogLevel
		logger = zmq4.NewLogger(options.LogLevel)
	} else {
		// Backward compatibility with boolean flags
		if options.LogInfo {
			logger = zmq4.NewLogger(zmq4.LogLevelInfo)
		} else if options.LogErrors {
			logger = zmq4.NewLogger(zmq4.LogLevelError)
		} else {
			logger = zmq4.DevNullLogger
		}
	}
	
	return &Broker{
		endpoint:          endpoint,
		heartbeatLiveness: options.HeartbeatLiveness,
		heartbeatInterval: options.HeartbeatInterval,
		heartbeatExpiry:   options.HeartbeatInterval * time.Duration(options.HeartbeatLiveness),
		requestTimeout:    options.RequestTimeout,
		options:           options,
		logger:            logger,
		ctx:               ctx,
		cancel:            cancel,
		services:          make(map[ServiceName]*Service),
		workers:           make(map[string]*BrokerWorker),
		stopCh:            make(chan struct{}),
		workerCh:          make(chan *Message, 1000),
		clientCh:          make(chan *Message, 1000),
		heartbeatCh:       make(chan struct{}, 1),
	}
}

// SetHeartbeat configures broker heartbeat settings
func (b *Broker) SetHeartbeat(liveness int, interval time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	b.heartbeatLiveness = liveness
	b.heartbeatInterval = interval
	b.heartbeatExpiry = interval * time.Duration(liveness)
}

// SetRequestTimeout sets the maximum time to wait for a worker response
func (b *Broker) SetRequestTimeout(timeout time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	b.requestTimeout = timeout
}

// Start starts the broker
func (b *Broker) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if b.running {
		return fmt.Errorf("mdp: broker already running")
	}
	
	// Create router socket with security if configured
	var socket zmq4.Socket
	if b.options.Security != nil {
		socket = zmq4.NewRouter(b.ctx, zmq4.WithSecurity(b.options.Security))
	} else {
		socket = zmq4.NewRouter(b.ctx)
	}
	
	err := socket.Listen(b.endpoint)
	if err != nil {
		return fmt.Errorf("mdp: failed to bind broker socket: %w", err)
	}
	
	b.socket = socket
	b.running = true
	
	// Start goroutines
	go b.heartbeatLoop()
	go b.messageLoop()
	go b.cleanupLoop()
	
	if b.options.Security != nil {
		b.logger.Info("MDP broker started on %s with %s security", b.endpoint, b.options.Security.Type())
	} else {
		b.logger.Info("MDP broker started on %s", b.endpoint)
	}
	return nil
}

// Stop stops the broker
func (b *Broker) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	if !b.running {
		return fmt.Errorf("mdp: broker not running")
	}
	
	b.running = false
	b.cancel()
	close(b.stopCh)
	
	if b.socket != nil {
		err := b.socket.Close()
		if err != nil {
			return fmt.Errorf("mdp: failed to close broker socket: %w", err)
		}
	}
	
	b.logger.Info("MDP broker stopped")
	return nil
}

// messageLoop handles incoming messages
func (b *Broker) messageLoop() {
	b.logger.Debug("MDP broker: messageLoop started - listening for ALL incoming messages")
	
	for {
		select {
		case <-b.stopCh:
			b.logger.Debug("MDP broker: messageLoop stopping")
			return
		default:
			if b.options.LogInfo {
				log.Printf("MDP broker: calling socket.Recv() to wait for message")
			}
			
			msg, err := b.socket.Recv()
			if err != nil {
				if b.running {
					b.logger.Error("MDP broker recv error: %v", err)
				}
				continue
			}
			
			if b.options.LogInfo {
				log.Printf("MDP broker: ★ RECEIVED MESSAGE with %d frames", len(msg.Frames))
				log.Printf("MDP broker: Raw message frames:")
				for i, frame := range msg.Frames {
					if i == 0 {
						log.Printf("  frame[%d] (identity): %x -> %q (len=%d)", i, frame, string(frame), len(frame))
					} else if len(frame) > 0 {
						log.Printf("  frame[%d]: %q (len=%d)", i, string(frame), len(frame))
					} else {
						log.Printf("  frame[%d]: <empty> (len=%d)", i, len(frame))
					}
				}
			}
			
			b.processMessage(msg)
		}
	}
}

// processMessage processes an incoming message
func (b *Broker) processMessage(msg zmq4.Msg) {
	frames := msg.Frames
	if len(frames) < 1 {
		b.logger.Warn("MDP broker: empty message received")
		return
	}
	
	// First frame is sender identity
	sender := frames[0]
	
	// Debug: log frame structure
	if b.options.LogInfo {
		log.Printf("MDP broker: received %d frames from %x", len(frames), sender)
		for i, frame := range frames {
			if len(frame) > 0 {
				log.Printf("  frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	if len(frames) < 3 {
		log.Printf("MDP broker: message too short from %x", sender)
		return
	}
	
	// Analyze message structure using frame separator detection
	msgType, protocolFrame, messageFrames := b.analyzeMessageStructure(frames, sender)
	
	if msgType == messageTypeUnknown {
		return // Error already logged in analyzeMessageStructure
	}
	
	if string(protocolFrame) == WorkerProtocol {
		if b.options.LogInfo {
			log.Printf("MDP broker: → processing worker message from %x", sender)
		}
		b.processWorkerMessage(sender, messageFrames)
	} else if string(protocolFrame) == ClientProtocol {
		if b.options.LogInfo {
			log.Printf("MDP broker: → processing client message from %x", sender)
		}
		b.processClientMessage(sender, messageFrames)
	} else {
		log.Printf("MDP broker: unknown protocol %q from %x", string(protocolFrame), sender)
	}
}

// splitOnSeparators splits frames into parts using empty frames as separators
// Returns array of frame parts, with empty separators removed
func (b *Broker) splitOnSeparators(frames [][]byte) [][][]byte {
	if len(frames) == 0 {
		return nil
	}
	
	var parts [][][]byte
	var currentPart [][]byte
	
	for _, frame := range frames {
		if len(frame) == 0 {
			// Empty frame is a separator
			if len(currentPart) > 0 {
				parts = append(parts, currentPart)
				currentPart = nil
			}
		} else {
			// Non-empty frame goes into current part
			currentPart = append(currentPart, frame)
		}
	}
	
	// Add final part if it exists
	if len(currentPart) > 0 {
		parts = append(parts, currentPart)
	}
	
	return parts
}

// analyzeMessageStructure analyzes frame structure using separator-based splitting
// Returns: messageType, protocolFrame, messageFrames
func (b *Broker) analyzeMessageStructure(frames [][]byte, sender []byte) (int, []byte, [][]byte) {
	if len(frames) < 2 {
		if b.options.LogErrors {
			log.Printf("MDP broker: insufficient frames from %x", sender)
		}
		return messageTypeUnknown, nil, nil
	}
	
	// Skip identity frame (first frame from ROUTER socket)
	workingFrames := frames[1:]
	
	// Debug logging if enabled
	if b.options.LogInfo {
		log.Printf("MDP broker: analyzing %d frames from %x", len(frames), sender)
		for i, frame := range frames {
			if len(frame) > 0 {
				log.Printf("  frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	// Split frames using separators and detect message type
	return b.detectMessageType(workingFrames, sender)
}

// detectMessageType uses separator-based splitting to identify message structure
func (b *Broker) detectMessageType(workingFrames [][]byte, sender []byte) (int, []byte, [][]byte) {
	if len(workingFrames) < 2 {
		if b.options.LogErrors {
			log.Printf("MDP broker: insufficient working frames from %x", sender)
		}
		return messageTypeUnknown, nil, nil
	}
	
	// Split frames into parts using empty frame separators
	parts := b.splitOnSeparators(workingFrames)
	
	if b.options.LogInfo {
		log.Printf("MDP broker: split into %d parts from %x", len(parts), sender)
		for i, part := range parts {
			log.Printf("  part[%d]: %d frames", i, len(part))
			for j, frame := range part {
				log.Printf("    frame[%d]: %q", j, string(frame))
			}
		}
	}
	
	// Look for protocol identifiers in the parts
	for _, part := range parts {
		if len(part) >= 1 {
			protocolFrame := part[0]
			protocolStr := string(protocolFrame)
			
			if protocolStr == WorkerProtocol {
				if b.options.LogInfo {
					log.Printf("MDP broker: detected worker message from %x", sender)
				}
				// Reconstruct the complete worker message from all parts
				// Format: [empty][protocol][command][client_addr][empty][body...]
				messageFrames := [][]byte{{}} // Start with empty separator
				
				// Add the first part (protocol + command + client_addr)
				messageFrames = append(messageFrames, part...)
				
				// Add remaining parts with empty separators between them
				for i := 1; i < len(parts); i++ {
					messageFrames = append(messageFrames, []byte{}) // Empty separator
					messageFrames = append(messageFrames, parts[i]...)
				}
				
				return messageTypeWorker, protocolFrame, messageFrames
				
			} else if protocolStr == ClientProtocol {
				if b.options.LogInfo {
					log.Printf("MDP broker: detected client message from %x", sender)
				}
				// Reconstruct message frames with proper separator
				// Format: [empty][protocol][service][body...]
				messageFrames := [][]byte{{}} // Start with empty separator
				messageFrames = append(messageFrames, part...)
				return messageTypeClient, protocolFrame, messageFrames
			}
		}
	}
	
	// Fallback: scan all frames directly for protocol identifiers
	// This handles cases where frames might not be properly separated
	for i, frame := range workingFrames {
		if len(frame) > 0 {
			frameStr := string(frame)
			if frameStr == WorkerProtocol || frameStr == ClientProtocol {
				if b.options.LogInfo {
					log.Printf("MDP broker: detected %s message (fallback) from %x", 
						map[string]string{WorkerProtocol: "worker", ClientProtocol: "client"}[frameStr], sender)
				}
				
				// Find the start of the message (should be preceded by empty frame)
				startIdx := i
				if i > 0 && len(workingFrames[i-1]) == 0 {
					startIdx = i - 1
				} else {
					// No empty frame before protocol, insert one
					messageFrames := [][]byte{{}} // Start with empty separator
					messageFrames = append(messageFrames, workingFrames[i:]...)
					if frameStr == WorkerProtocol {
						return messageTypeWorker, frame, messageFrames
					} else {
						return messageTypeClient, frame, messageFrames
					}
				}
				
				if frameStr == WorkerProtocol {
					return messageTypeWorker, frame, workingFrames[startIdx:]
				} else {
					return messageTypeClient, frame, workingFrames[startIdx:]
				}
			}
		}
	}
	
	// Unable to detect message type
	if b.options.LogErrors {
		log.Printf("MDP broker: unrecognized message format from %x", sender)
		for i, frame := range workingFrames {
			if len(frame) > 0 {
				log.Printf("  working_frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  working_frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	return messageTypeUnknown, nil, nil
}

// processWorkerMessage processes a message from a worker
func (b *Broker) processWorkerMessage(identity []byte, frames [][]byte) {
	workerMsg, err := ParseWorkerMessage(frames)
	if err != nil {
		log.Printf("MDP broker: invalid worker message from %x: %v", identity, err)
		return
	}
	
	workerKey := string(identity)
	
	b.mu.Lock()
	defer b.mu.Unlock()
	
	worker, exists := b.workers[workerKey]
	if b.options.LogInfo {
		log.Printf("MDP broker: processing worker command %s - worker %s exists=%t", 
			workerMsg.Command, workerKey[:8], exists)
		if exists {
			// Check if worker is in available list for logging
			service := b.getOrCreateService(worker.Service)
			available := b.isWorkerInAvailableList(worker, service)
			log.Printf("MDP broker: worker %s state - Available=%t, InstanceID=%s, ptr=%p", 
				workerKey[:8], available, worker.InstanceID, worker)
		}
	}
	
	switch workerMsg.Command {
	case WorkerReady:
		if exists {
			// Worker sent READY when already registered - disconnect
			if b.options.LogInfo {
				// Check if worker is in available list for logging
				service := b.getOrCreateService(worker.Service)
				available := b.isWorkerInAvailableList(worker, service)
				log.Printf("MDP broker: worker %x already exists - disconnecting (Available was %t, InstanceID was %s)", 
					identity, available, worker.InstanceID)
			}
			b.disconnectWorker(worker, true)
		} else {
			// Register new worker
			now := time.Now()
			expiry := now.Add(b.heartbeatExpiry)
			instanceID := fmt.Sprintf("worker-%d", now.UnixNano())
			worker = &BrokerWorker{
				ID:       WorkerID(identity),
				Service:  workerMsg.Service,
				Identity: identity,
				Address:  identity,
				Expiry:   expiry,
				InstanceID: instanceID,
				LastSeen: now,
			}
			
			// Add worker to available list for this service
			service := b.getOrCreateService(worker.Service)
			service.AvailableWorkers = append(service.AvailableWorkers, worker)
			service.ActiveWorkers++
			
			if b.options.LogInfo {
				log.Printf("MDP broker: worker %x marked as Available = true (new registration), expires at %v (heartbeatExpiry=%v) instanceID=%s", 
					identity, expiry, b.heartbeatExpiry, instanceID)
			}
			
			b.workers[workerKey] = worker
			b.totalWorkers++
			
			// Add to service
			b.addWorkerToService(worker)
			
			if b.options.LogInfo {
				log.Printf("MDP broker: worker %x registered for service %s", identity, workerMsg.Service)
			}
		}
		
	case WorkerReply:
		if b.options.LogInfo {
			log.Printf("MDP broker: ← received REPLY from worker %x", identity)
		}
		if !exists {
			// Unknown worker - disconnect
			if b.options.LogErrors {
				log.Printf("MDP broker: unknown worker %x sent reply - disconnecting", identity)
			}
			if err := b.sendToWorker(identity, WorkerDisconnect, nil, nil); err != nil {
				if b.options.LogErrors {
					log.Printf("MDP broker: failed to send disconnect to unknown worker %x: %v", identity, err)
				}
			}
		} else {
			if b.options.LogInfo {
				log.Printf("MDP broker: forwarding reply from worker %x to client %x", identity, workerMsg.ClientAddr)
			}
			// Forward reply to client (use worker's service, not message service)
			b.sendToClient(workerMsg.ClientAddr, worker.Service, workerMsg.Body)
			worker.LastSeen = time.Now()
			worker.Expiry = time.Now().Add(b.heartbeatExpiry)
			worker.RequestsProcessed++
			b.totalReplies++
			
			// Move worker from busy back to available
			service := b.getOrCreateService(worker.Service)
			b.moveWorkerToAvailable(worker, service)
			if b.options.LogInfo {
				log.Printf("MDP broker: worker %x moved back to Available list (completed request) instanceID=%s", 
					identity, worker.InstanceID)
			}
		}
		
	case WorkerHeartbeat:
		if exists {
			worker.LastSeen = time.Now()
			oldExpiry := worker.Expiry
			worker.Expiry = time.Now().Add(b.heartbeatExpiry)
			if b.options.LogInfo {
				log.Printf("MDP broker: worker %x heartbeat - updated expiry from %v to %v (heartbeatExpiry=%v)", 
					identity, oldExpiry, worker.Expiry, b.heartbeatExpiry)
			}
		} else {
			// Unknown worker - disconnect
			if err := b.sendToWorker(identity, WorkerDisconnect, nil, nil); err != nil {
				if b.options.LogErrors {
					log.Printf("MDP broker: failed to send disconnect to unknown worker %x: %v", identity, err)
				}
			}
		}
		
	case WorkerDisconnect:
		if exists {
			b.disconnectWorker(worker, false)
		}
		
	default:
		log.Printf("MDP broker: unknown worker command %s from %x", workerMsg.Command, identity)
	}
}

// processClientMessage processes a message from a client
func (b *Broker) processClientMessage(identity []byte, frames [][]byte) {
	clientMsg, err := ParseClientMessage(frames)
	if err != nil {
		log.Printf("MDP broker: invalid client message from %x: %v", identity, err)
		return
	}
	
	b.mu.Lock()
	defer b.mu.Unlock()
	
	// Get or create service
	service := b.getOrCreateService(clientMsg.Service)
	
	// Debug: check worker state right before dispatching
	if b.options.LogInfo {
		totalAvailable := 0
		now := time.Now()
		log.Printf("MDP broker: CLIENT REQUEST RECEIVED for service %s at %v", clientMsg.Service, now)
		for workerKey, w := range b.workers {
			expired := w.IsExpired()
			timeUntilExpiry := w.Expiry.Sub(now)
			// Check if worker is available using list-based system
			service := b.getOrCreateService(w.Service)
			available := b.isWorkerInAvailableList(w, service)
			if available && !expired {
				totalAvailable++
			}
			log.Printf("MDP broker: worker %s - Available=%t, Expired=%t, TimeUntilExpiry=%v, Service=%s, InstanceID=%s", 
				workerKey[:8], available, expired, timeUntilExpiry, w.Service, w.InstanceID)
			
			// Additional debug: check if service matches
			if w.Service == clientMsg.Service {
				log.Printf("MDP broker: worker %s serves matching service %s - Available=%t, Expired=%t", 
					workerKey[:8], w.Service, available, expired)
			}
		}
		log.Printf("MDP broker: about to dispatch for service %s - %d total workers, %d available", 
			clientMsg.Service, len(b.workers), totalAvailable)
	}
	
	// Create pending request
	request := &PendingRequest{
		Client:  identity,
		Service: clientMsg.Service,
		Body:    clientMsg.Body,
		Created: time.Now(),
	}
	
	service.Requests = append(service.Requests, request)
	service.TotalRequests++
	b.totalRequests++
	
	// The list-based system handles availability automatically
	
	// Try to dispatch immediately
	b.dispatchRequests(service)
	
	if b.options.LogInfo {
		log.Printf("MDP broker: request from client %x for service %s", identity, clientMsg.Service)
	}
}

// sendToWorker sends a message to a worker and returns error if delivery fails
func (b *Broker) sendToWorker(identity []byte, command string, clientAddr []byte, body []byte) error {
	if b.options.LogInfo {
		log.Printf("MDP broker: → sending %s (0x%02x) to worker %x", command, []byte(command)[0], identity)
	}
	
	msg := NewWorkerMessage(command, clientAddr, body)
	frames := msg.FormatWorkerMessage()
	
	// Prepend worker identity for ROUTER socket routing
	allFrames := make([][]byte, 0, len(frames)+1)
	allFrames = append(allFrames, identity)
	allFrames = append(allFrames, frames...)
	
	if b.options.LogInfo && command == WorkerRequest {
		log.Printf("MDP broker: sending %d frames to worker:", len(allFrames))
		for i, frame := range allFrames {
			if len(frame) > 0 {
				log.Printf("  frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	zmqMsg := zmq4.NewMsgFrom(allFrames...)
	
	// Add comprehensive socket state diagnostic logging
	if b.options.LogInfo && command == WorkerRequest {
		log.Printf("MDP broker: about to call socket.Send() for REQUEST message to worker %x", identity)
		log.Printf("MDP broker: socket type: %T", b.socket)
		log.Printf("MDP broker: message frames being sent: %d", len(allFrames))
		for i, frame := range allFrames {
			if i == 0 {
				log.Printf("  frame[%d] (ROUTER identity): %x (len=%d)", i, frame, len(frame))
			} else if len(frame) > 0 {
				log.Printf("  frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	err := b.socket.Send(zmqMsg)
	if err != nil {
		log.Printf("MDP broker: failed to send to worker %x: %v", identity, err)
		return fmt.Errorf("failed to send %s to worker %x: %w", command, identity, err)
	}
	
	if b.options.LogInfo {
		log.Printf("MDP broker: ✓ sent %s (0x%02x) to worker %x", command, []byte(command)[0], identity)
		if command == WorkerRequest {
			log.Printf("MDP broker: REQUEST message successfully transmitted through ROUTER socket")
			log.Printf("MDP broker: socket.Send() returned nil error - message should be routed to worker")
		}
	}
	return nil
}

// sendToClient sends a message to a client
func (b *Broker) sendToClient(identity []byte, service ServiceName, body []byte) {
	if b.options.LogInfo {
		log.Printf("MDP broker: → sending reply to client %x for service %s", identity, service)
	}
	
	msg := NewClientReply(service, body)
	frames := msg.FormatClientReply()
	
	// For ROUTER->REQ communication, prepend identity only
	// FormatClientReply() already includes the empty delimiter frame
	allFrames := make([][]byte, 0, len(frames)+1)
	allFrames = append(allFrames, identity)
	allFrames = append(allFrames, frames...)
	
	zmqMsg := zmq4.NewMsgFrom(allFrames...)
	
	if b.options.LogInfo {
		log.Printf("MDP broker: sending %d frames to client:", len(allFrames))
		for i, frame := range allFrames {
			if len(frame) > 0 {
				log.Printf("  frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	err := b.socket.Send(zmqMsg)
	if err != nil {
		log.Printf("MDP broker: failed to send to client %x: %v", identity, err)
	} else if b.options.LogInfo {
		log.Printf("MDP broker: ✓ sent reply to client %x", identity)
	}
}

// addWorkerToService adds a worker to a service
func (b *Broker) addWorkerToService(worker *BrokerWorker) {
	// Worker is already added to AvailableWorkers list during registration
	// This function is kept for backward compatibility but does nothing now
}

// isWorkerInAvailableList checks if a worker is in the available list for its service
func (b *Broker) isWorkerInAvailableList(worker *BrokerWorker, service *Service) bool {
	for _, w := range service.AvailableWorkers {
		if w == worker {
			return true
		}
	}
	return false
}

// moveWorkerToBusy moves a worker from available list to busy list
func (b *Broker) moveWorkerToBusy(worker *BrokerWorker, service *Service) {
	// Remove from available list
	for i, w := range service.AvailableWorkers {
		if w == worker {
			service.AvailableWorkers = append(service.AvailableWorkers[:i], service.AvailableWorkers[i+1:]...)
			break
		}
	}
	
	// Add to busy list
	service.BusyWorkers = append(service.BusyWorkers, worker)
}

// moveWorkerToAvailable moves a worker from busy list to available list
func (b *Broker) moveWorkerToAvailable(worker *BrokerWorker, service *Service) {
	// Remove from busy list
	for i, w := range service.BusyWorkers {
		if w == worker {
			service.BusyWorkers = append(service.BusyWorkers[:i], service.BusyWorkers[i+1:]...)
			break
		}
	}
	
	// Add to available list (only if not already there)
	if !b.isWorkerInAvailableList(worker, service) {
		service.AvailableWorkers = append(service.AvailableWorkers, worker)
	}
}


// getOrCreateService gets or creates a service
func (b *Broker) getOrCreateService(name ServiceName) *Service {
	service, exists := b.services[name]
	if !exists {
		service = &Service{
			Name:             name,
			Requests:         make([]*PendingRequest, 0),
			AvailableWorkers: make([]*BrokerWorker, 0),
			BusyWorkers:      make([]*BrokerWorker, 0),
		}
		b.services[name] = service
	}
	return service
}

// getAvailableWorkersForService returns workers that are available and serve the specific service
func (b *Broker) getAvailableWorkersForService(serviceName ServiceName) []*BrokerWorker {
	service := b.getOrCreateService(serviceName)
	
	// Filter out expired workers from the available list
	var availableWorkers []*BrokerWorker
	for _, worker := range service.AvailableWorkers {
		if !worker.IsExpired() {
			availableWorkers = append(availableWorkers, worker)
		}
	}
	
	// Count total available workers across all services for logging
	totalAvailable := 0
	for _, svc := range b.services {
		for _, worker := range svc.AvailableWorkers {
			if !worker.IsExpired() {
				totalAvailable++
			}
		}
	}
	
	if b.options.LogInfo {
		log.Printf("MDP broker: found %d total available workers, %d for service %s", 
			totalAvailable, len(availableWorkers), serviceName)
		for i, worker := range availableWorkers {
			log.Printf("MDP broker: available[%d]: worker %x serves %s", i, worker.Identity, worker.Service)
		}
	}
	
	return availableWorkers
}

// dispatchRequests dispatches pending requests to available workers for this service
func (b *Broker) dispatchRequests(service *Service) {
	// Filter out expired workers from the available list first
	var validWorkers []*BrokerWorker
	for _, worker := range service.AvailableWorkers {
		if !worker.IsExpired() {
			validWorkers = append(validWorkers, worker)
		}
	}
	service.AvailableWorkers = validWorkers
	
	if b.options.LogInfo {
		log.Printf("MDP broker: dispatchRequests - service %s has %d pending requests, %d available workers", 
			service.Name, len(service.Requests), len(service.AvailableWorkers))
	}
	
	for len(service.Requests) > 0 && len(service.AvailableWorkers) > 0 {
		// Get next request
		request := service.Requests[0]
		service.Requests = service.Requests[1:]
		
		// Get next available worker from the service's list
		worker := service.AvailableWorkers[0]
		
		// Move worker from available to busy list
		b.moveWorkerToBusy(worker, service)
		if b.options.LogInfo {
			log.Printf("MDP broker: worker %x moved to Busy list (assigned request) at %v instanceID=%s", 
				worker.Identity, time.Now(), worker.InstanceID)
		}
		
		// Send request to worker - rollback availability if send fails
		if b.options.LogInfo {
			log.Printf("MDP broker: about to call sendToWorker with command %q (len=%d, bytes=%v)", 
				WorkerRequest, len(WorkerRequest), []byte(WorkerRequest))
		}
		err := b.sendToWorker(worker.Identity, WorkerRequest, request.Client, request.Body)
		if err != nil {
			// Send failed - rollback worker from busy back to available
			b.moveWorkerToAvailable(worker, service)
			if b.options.LogErrors {
				log.Printf("MDP broker: failed to send request to worker %x: %v", worker.Identity, err)
			}
			// Put request back at front of queue for retry with another worker
			service.Requests = append([]*PendingRequest{request}, service.Requests...)
			continue // Try next available worker
		}
		
		if b.options.LogInfo {
			log.Printf("MDP broker: dispatched request to worker %x for service %s", worker.Identity, service.Name)
		}
	}
}

// disconnectWorker disconnects a worker
func (b *Broker) disconnectWorker(worker *BrokerWorker, send bool) {
	if send {
		if err := b.sendToWorker(worker.Identity, WorkerDisconnect, nil, nil); err != nil {
			if b.options.LogErrors {
				log.Printf("MDP broker: failed to send disconnect to worker %x: %v", worker.Identity, err)
			}
		}
	}
	
	// Remove from workers map
	workerKey := string(worker.Identity)
	delete(b.workers, workerKey)
	
	// Remove from service lists
	if service, exists := b.services[worker.Service]; exists {
		// Remove from available workers list
		for i, w := range service.AvailableWorkers {
			if w == worker {
				service.AvailableWorkers = append(service.AvailableWorkers[:i], service.AvailableWorkers[i+1:]...)
				break
			}
		}
		// Remove from busy workers list
		for i, w := range service.BusyWorkers {
			if w == worker {
				service.BusyWorkers = append(service.BusyWorkers[:i], service.BusyWorkers[i+1:]...)
				break
			}
		}
		service.ActiveWorkers--
	}
	
	if b.options.LogInfo {
		log.Printf("MDP broker: disconnected worker %x from service %s", worker.Identity, worker.Service)
	}
}

// heartbeatLoop sends periodic heartbeats to workers
func (b *Broker) heartbeatLoop() {
	ticker := time.NewTicker(b.heartbeatInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			b.sendHeartbeats()
		case <-b.stopCh:
			return
		}
	}
}

// sendHeartbeats sends heartbeat messages to all workers
func (b *Broker) sendHeartbeats() {
	b.mu.RLock()
	workers := make([]*BrokerWorker, 0, len(b.workers))
	for _, worker := range b.workers {
		workers = append(workers, worker)
	}
	b.mu.RUnlock()
	
	for _, worker := range workers {
		if err := b.sendToWorker(worker.Identity, WorkerHeartbeat, nil, nil); err != nil {
			if b.options.LogErrors {
				log.Printf("MDP broker: failed to send heartbeat to worker %x: %v", worker.Identity, err)
			}
		}
	}
}

// cleanupLoop removes expired workers and requests
func (b *Broker) cleanupLoop() {
	ticker := time.NewTicker(b.heartbeatInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			b.cleanup()
		case <-b.stopCh:
			return
		}
	}
}

// cleanup removes expired workers and requests
func (b *Broker) cleanup() {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	// Count total available workers across all services
	totalAvailable := 0
	for _, service := range b.services {
		for _, w := range service.AvailableWorkers {
			if !w.IsExpired() {
				totalAvailable++
			}
		}
	}
	
	if b.options.LogInfo {
		log.Printf("MDP broker: cleanup running - %d workers, %d available", len(b.workers), totalAvailable)
	}
	
	// Remove expired workers
	for key, worker := range b.workers {
		if worker.IsExpired() {
			if b.options.LogInfo {
				log.Printf("MDP broker: removing expired worker %x from service %s (expired at %v, now %v)", 
					worker.Identity, worker.Service, worker.Expiry, time.Now())
			}
			delete(b.workers, key)
			
			// Remove from service lists
			if service, exists := b.services[worker.Service]; exists {
				// Remove from available workers list
				for i, w := range service.AvailableWorkers {
					if w == worker {
						service.AvailableWorkers = append(service.AvailableWorkers[:i], service.AvailableWorkers[i+1:]...)
						break
					}
				}
				// Remove from busy workers list
				for i, w := range service.BusyWorkers {
					if w == worker {
						service.BusyWorkers = append(service.BusyWorkers[:i], service.BusyWorkers[i+1:]...)
						break
					}
				}
				service.ActiveWorkers--
			}
		}
	}
	
	// Remove expired requests
	for _, service := range b.services {
		validRequests := make([]*PendingRequest, 0, len(service.Requests))
		for _, request := range service.Requests {
			if !request.IsExpired(b.requestTimeout) {
				validRequests = append(validRequests, request)
			} else {
				if b.options.LogInfo {
					log.Printf("MDP broker: expired request from client %x for service %s", request.Client, request.Service)
				}
			}
		}
		service.Requests = validRequests
	}
}

// GetStats returns broker statistics
func (b *Broker) GetStats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()
	
	serviceStats := make(map[string]interface{})
	for name, service := range b.services {
		serviceStats[string(name)] = map[string]interface{}{
			"active_workers":  service.ActiveWorkers,
			"pending_requests": len(service.Requests),
			"total_requests":   service.TotalRequests,
			"total_responses":  service.TotalResponses,
		}
	}
	
	// Count available workers across all services
	availableWorkers := 0
	for _, service := range b.services {
		for _, worker := range service.AvailableWorkers {
			if !worker.IsExpired() {
				availableWorkers++
			}
		}
	}
	
	return map[string]interface{}{
		"total_clients":     b.totalClients,
		"total_workers":     b.totalWorkers,
		"total_requests":    b.totalRequests,
		"total_replies":     b.totalReplies,
		"active_workers":    len(b.workers),
		"available_workers": availableWorkers,
		"services":          serviceStats,
	}
}