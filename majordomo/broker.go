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

	"github.com/destiny/zmq4"
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
	Name     ServiceName
	Requests []*PendingRequest
	Workers  []*BrokerWorker
	
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
	waiting  []*BrokerWorker          // Workers waiting for requests
	
	// Statistics
	totalClients  uint64
	totalWorkers  uint64
	totalRequests uint64
	totalReplies  uint64
	
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
	
	return &Broker{
		endpoint:          endpoint,
		heartbeatLiveness: options.HeartbeatLiveness,
		heartbeatInterval: options.HeartbeatInterval,
		heartbeatExpiry:   options.HeartbeatInterval * time.Duration(options.HeartbeatLiveness),
		requestTimeout:    options.RequestTimeout,
		options:           options,
		ctx:               ctx,
		cancel:            cancel,
		services:          make(map[ServiceName]*Service),
		workers:           make(map[string]*BrokerWorker),
		waiting:           make([]*BrokerWorker, 0),
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
	
	if b.options.LogInfo {
		if b.options.Security != nil {
			log.Printf("MDP broker started on %s with %s security", b.endpoint, b.options.Security.Type())
		} else {
			log.Printf("MDP broker started on %s", b.endpoint)
		}
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
	
	if b.options.LogInfo {
		log.Printf("MDP broker stopped")
	}
	return nil
}

// messageLoop handles incoming messages
func (b *Broker) messageLoop() {
	for {
		select {
		case <-b.stopCh:
			return
		default:
			msg, err := b.socket.Recv()
			if err != nil {
				if b.running && b.options.LogErrors {
					log.Printf("MDP broker recv error: %v", err)
				}
				continue
			}
			
			b.processMessage(msg)
		}
	}
}

// processMessage processes an incoming message
func (b *Broker) processMessage(msg zmq4.Msg) {
	frames := msg.Frames
	if len(frames) < 1 {
		log.Printf("MDP broker: empty message received")
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

// analyzeMessageStructure analyzes frame structure using separator detection
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
	
	// Find empty frame separators
	separatorIndices := []int{}
	for i, frame := range workingFrames {
		if len(frame) == 0 {
			separatorIndices = append(separatorIndices, i)
		}
	}
	
	// Analyze separator patterns to detect message type
	return b.detectMessageTypeFromSeparators(workingFrames, separatorIndices, sender)
}

// detectMessageTypeFromSeparators uses separator patterns to identify message structure
func (b *Broker) detectMessageTypeFromSeparators(workingFrames [][]byte, separatorIndices []int, sender []byte) (int, []byte, [][]byte) {
	if len(workingFrames) < 2 {
		if b.options.LogErrors {
			log.Printf("MDP broker: insufficient working frames from %x", sender)
		}
		return messageTypeUnknown, nil, nil
	}
	
	// Pattern 1: Worker message [empty][protocol][command][...]
	// This has exactly one separator at the beginning
	if len(separatorIndices) >= 1 && separatorIndices[0] == 0 {
		if len(workingFrames) >= 2 && len(workingFrames[1]) > 0 {
			protocolFrame := workingFrames[1]
			if string(protocolFrame) == WorkerProtocol {
				if b.options.LogInfo {
					log.Printf("MDP broker: detected worker message from %x", sender)
				}
				return messageTypeWorker, protocolFrame, workingFrames
			}
		}
	}
	
	// Pattern 2: Client message - scan for protocol after separators
	// Client messages may have multiple separators: [empty...][protocol][service][body...]
	if len(separatorIndices) > 0 {
		// Look for protocol frame after the last separator
		lastSeparatorIdx := separatorIndices[len(separatorIndices)-1]
		
		// Scan frames after last separator for protocol
		for i := lastSeparatorIdx + 1; i < len(workingFrames); i++ {
			if len(workingFrames[i]) > 0 {
				protocolFrame := workingFrames[i]
				if string(protocolFrame) == ClientProtocol {
					if b.options.LogInfo {
						log.Printf("MDP broker: detected client message from %x", sender)
					}
					// For client parsing, we need [empty][protocol][service][body...]
					// Find the separator immediately before the protocol frame
					protocolStartIdx := i - 1
					if protocolStartIdx >= 0 && len(workingFrames[protocolStartIdx]) == 0 {
						return messageTypeClient, protocolFrame, workingFrames[protocolStartIdx:]
					} else {
						// No empty frame before protocol, create the expected structure
						clientFrames := [][]byte{{}} // Start with empty frame
						clientFrames = append(clientFrames, workingFrames[i:]...) // Add protocol and rest
						return messageTypeClient, protocolFrame, clientFrames
					}
				}
				// If we find a non-empty frame that's not a protocol, this isn't a valid message
				break
			}
		}
	}
	
	// Pattern 3: Fallback - scan all frames for protocols (handles unknown separator patterns)
	for i, frame := range workingFrames {
		if len(frame) > 0 {
			frameStr := string(frame)
			if frameStr == WorkerProtocol {
				if b.options.LogInfo {
					log.Printf("MDP broker: detected worker message (fallback) from %x", sender)
				}
				// For worker, we need the empty frame before protocol
				if i > 0 {
					return messageTypeWorker, frame, workingFrames[i-1:]
				} else {
					return messageTypeWorker, frame, workingFrames
				}
			} else if frameStr == ClientProtocol {
				if b.options.LogInfo {
					log.Printf("MDP broker: detected client message (fallback) from %x", sender)
				}
				// For client, find the separator before protocol
				for j := i - 1; j >= 0; j-- {
					if len(workingFrames[j]) == 0 {
						return messageTypeClient, frame, workingFrames[j:]
					}
				}
				// No separator found, use from beginning
				return messageTypeClient, frame, workingFrames
			}
		}
	}
	
	// Unable to detect message type
	if b.options.LogErrors {
		log.Printf("MDP broker: unrecognized message format from %x", sender)
		log.Printf("  separators at indices: %v", separatorIndices)
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
	
	switch workerMsg.Command {
	case WorkerReady:
		if exists {
			// Worker sent READY when already registered - disconnect
			b.disconnectWorker(worker, true)
		} else {
			// Register new worker
			worker = &BrokerWorker{
				ID:       WorkerID(identity),
				Service:  workerMsg.Service,
				Identity: identity,
				Address:  identity,
				Expiry:   time.Now().Add(b.heartbeatExpiry),
				LastSeen: time.Now(),
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
			b.sendToWorker(identity, WorkerDisconnect, nil, nil)
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
			
			// Worker is now available for more requests
			b.addWorkerToWaiting(worker)
			if b.options.LogInfo {
				log.Printf("MDP broker: worker %x now available for more requests", identity)
			}
		}
		
	case WorkerHeartbeat:
		if exists {
			worker.LastSeen = time.Now()
			worker.Expiry = time.Now().Add(b.heartbeatExpiry)
		} else {
			// Unknown worker - disconnect
			b.sendToWorker(identity, WorkerDisconnect, nil, nil)
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
	
	// Try to dispatch immediately
	b.dispatchRequests(service)
	
	if b.options.LogInfo {
		log.Printf("MDP broker: request from client %x for service %s", identity, clientMsg.Service)
	}
}

// sendToWorker sends a message to a worker
func (b *Broker) sendToWorker(identity []byte, command string, clientAddr []byte, body []byte) {
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
	
	err := b.socket.Send(zmqMsg)
	if err != nil {
		log.Printf("MDP broker: failed to send to worker %x: %v", identity, err)
	} else if b.options.LogInfo {
		log.Printf("MDP broker: ✓ sent %s (0x%02x) to worker %x", command, []byte(command)[0], identity)
	}
}

// sendToClient sends a message to a client
func (b *Broker) sendToClient(identity []byte, service ServiceName, body []byte) {
	if b.options.LogInfo {
		log.Printf("MDP broker: → sending reply to client %x for service %s", identity, service)
	}
	
	msg := NewClientReply(service, body)
	frames := msg.FormatClientReply()
	
	// For ROUTER->REQ communication, prepend identity and empty frame for routing
	allFrames := make([][]byte, 0, len(frames)+2)
	allFrames = append(allFrames, identity)
	allFrames = append(allFrames, []byte{}) // Empty frame separator
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
	service := b.getOrCreateService(worker.Service)
	service.Workers = append(service.Workers, worker)
	service.ActiveWorkers++
	
	// Worker is initially available
	b.addWorkerToWaiting(worker)
}

// addWorkerToWaiting adds a worker to the waiting queue
func (b *Broker) addWorkerToWaiting(worker *BrokerWorker) {
	// Remove from waiting queue first (in case already there)
	b.removeWorkerFromWaiting(worker)
	
	// Add to end of waiting queue
	b.waiting = append(b.waiting, worker)
}

// removeWorkerFromWaiting removes a worker from the waiting queue
func (b *Broker) removeWorkerFromWaiting(worker *BrokerWorker) {
	for i, w := range b.waiting {
		if w == worker {
			// Remove from slice
			b.waiting = append(b.waiting[:i], b.waiting[i+1:]...)
			break
		}
	}
}

// getOrCreateService gets or creates a service
func (b *Broker) getOrCreateService(name ServiceName) *Service {
	service, exists := b.services[name]
	if !exists {
		service = &Service{
			Name:     name,
			Requests: make([]*PendingRequest, 0),
			Workers:  make([]*BrokerWorker, 0),
		}
		b.services[name] = service
	}
	return service
}

// dispatchRequests dispatches pending requests to available workers
func (b *Broker) dispatchRequests(service *Service) {
	for len(service.Requests) > 0 && len(b.waiting) > 0 {
		// Get next request
		request := service.Requests[0]
		service.Requests = service.Requests[1:]
		
		// Get next available worker
		worker := b.waiting[0]
		b.waiting = b.waiting[1:]
		
		// Send request to worker
		b.sendToWorker(worker.Identity, WorkerRequest, request.Client, request.Body)
		
			if b.options.LogInfo {
			log.Printf("MDP broker: dispatched request to worker %x for service %s", worker.Identity, service.Name)
		}
	}
}

// disconnectWorker disconnects a worker
func (b *Broker) disconnectWorker(worker *BrokerWorker, send bool) {
	if send {
		b.sendToWorker(worker.Identity, WorkerDisconnect, nil, nil)
	}
	
	// Remove from workers map
	workerKey := string(worker.Identity)
	delete(b.workers, workerKey)
	
	// Remove from waiting queue
	b.removeWorkerFromWaiting(worker)
	
	// Remove from service
	if service, exists := b.services[worker.Service]; exists {
		for i, w := range service.Workers {
			if w == worker {
				service.Workers = append(service.Workers[:i], service.Workers[i+1:]...)
				service.ActiveWorkers--
				break
			}
		}
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
		b.sendToWorker(worker.Identity, WorkerHeartbeat, nil, nil)
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
	
	// Remove expired workers
	for key, worker := range b.workers {
		if worker.IsExpired() {
			delete(b.workers, key)
			b.removeWorkerFromWaiting(worker)
			
			// Remove from service
			if service, exists := b.services[worker.Service]; exists {
				for i, w := range service.Workers {
					if w == worker {
						service.Workers = append(service.Workers[:i], service.Workers[i+1:]...)
						service.ActiveWorkers--
						break
					}
				}
			}
			
			if b.options.LogInfo {
				log.Printf("MDP broker: expired worker %x from service %s", worker.Identity, worker.Service)
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
	
	return map[string]interface{}{
		"total_clients":    b.totalClients,
		"total_workers":    b.totalWorkers,
		"total_requests":   b.totalRequests,
		"total_replies":    b.totalReplies,
		"active_workers":   len(b.workers),
		"waiting_workers":  len(b.waiting),
		"services":         serviceStats,
	}
}