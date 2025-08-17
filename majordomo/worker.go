// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package majordomo

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/destiny/zmq4/v25"
)

// RequestHandler is a function that processes worker requests
type RequestHandler func(request []byte) ([]byte, error)

// workerMessage represents an internal message to be sent to the broker
type workerMessage struct {
	command    string
	clientAddr []byte
	body       []byte
}

// stateUpdateType represents different types of state updates
type stateUpdateType int

const (
	stateConnected stateUpdateType = iota
	stateDisconnected
	stateExpectReply
	stateReplyComplete
	stateSetReplyTo
	stateSetLiveness
	stateDecreaseLiveness
	stateSetHeartbeatTime
	stateSetRunning
	stateSetSocketReady
	stateSetSocketClosed
)

// stateUpdate represents a state change message
type stateUpdate struct {
	updateType stateUpdateType
	data       interface{} // For replyTo address, liveness value, etc.
}

// statsUpdateType represents different types of statistics updates
type statsUpdateType int

const (
	statsIncRequests statsUpdateType = iota
	statsIncReplies
	statsIncErrors
	statsIncReconnects
)

// statsUpdate represents a statistics update message
type statsUpdate struct {
	updateType statsUpdateType
}

// queryType represents different types of queries
type queryType int

const (
	queryIsConnected queryType = iota
	queryIsRunning
	queryGetStats
	queryGetExpectReply
	queryGetReplyTo
	queryGetHeartbeatTime
	queryGetLiveness
	queryIsSocketReady
)

// queryRequest represents a request for current state/stats
type queryRequest struct {
	queryType  queryType
	responseCh chan interface{}
}

// WorkerOptions configures MDP worker behavior
type WorkerOptions struct {
	HeartbeatLiveness int           // Heartbeat liveness factor
	HeartbeatInterval time.Duration // Heartbeat interval
	ReconnectInterval time.Duration // Reconnection interval
	Security          zmq4.Security // Security mechanism (nil for no security)
	LogErrors         bool          // Whether to log errors
	LogInfo           bool          // Whether to log info messages
}

// DefaultWorkerOptions returns default worker options
func DefaultWorkerOptions() *WorkerOptions {
	return &WorkerOptions{
		HeartbeatLiveness: DefaultHeartbeatLiveness,
		HeartbeatInterval: DefaultHeartbeatInterval,
		ReconnectInterval: 2500 * time.Millisecond,
		Security:          nil,
		LogErrors:         true,
		LogInfo:           false,
	}
}

// Worker implements the MDP worker
type Worker struct {
	// Configuration
	service        ServiceName
	brokerEndpoint string
	options        *WorkerOptions
	handler        RequestHandler
	
	// Networking
	socket   zmq4.Socket
	ctx      context.Context
	cancel   context.CancelFunc
	
	// State management
	mu              sync.RWMutex
	running         bool
	connected       bool
	expectReply     bool
	replyTo         []byte
	heartbeatAt     time.Time
	liveness        int
	
	// Control channels
	stopCh      chan struct{}
	heartbeatCh chan struct{}
	
	// Socket communication channels (lifecycle managed)
	socketMu        sync.RWMutex  // Protects socket communication channels
	sendCh          chan workerMessage
	recvCh          chan zmq4.Msg
	errorCh         chan error
	closeCh         chan struct{}
	socketDone      chan struct{}
	socketManagerID uint64  // Unique ID for socket manager sessions
	
	// State management channels
	stateUpdateCh chan stateUpdate
	statsUpdateCh chan statsUpdate
	queryCh       chan queryRequest
	stateDone     chan struct{}
	stateStopped  int32  // Atomic flag to ensure state manager is only stopped once
	
	// Statistics
	totalRequests uint64
	totalReplies  uint64
	totalErrors   uint64
	reconnects    uint64
}

// NewWorker creates a new MDP worker
func NewWorker(service ServiceName, brokerEndpoint string, handler RequestHandler, options *WorkerOptions) (*Worker, error) {
	if err := service.Validate(); err != nil {
		return nil, fmt.Errorf("mdp: %w", err)
	}
	
	if handler == nil {
		return nil, fmt.Errorf("mdp: request handler cannot be nil")
	}
	
	if options == nil {
		options = DefaultWorkerOptions()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	w := &Worker{
		service:        service,
		brokerEndpoint: brokerEndpoint,
		options:        options,
		handler:        handler,
		ctx:            ctx,
		cancel:         cancel,
		liveness:       options.HeartbeatLiveness,
		stopCh:         make(chan struct{}),
		heartbeatCh:    make(chan struct{}, 1),
		// Socket communication channels will be created in initSocketChannels()
		socketManagerID: 0,
		stateUpdateCh:  make(chan stateUpdate, 10),
		statsUpdateCh:  make(chan statsUpdate, 10),
		queryCh:        make(chan queryRequest, 5),
		stateDone:      make(chan struct{}),
	}
	
	// Start state manager immediately so queries work
	go w.stateManager()
	
	// Initialize socket channels for first use
	w.initSocketChannels()
	
	return w, nil
}

// initSocketChannels initializes or reinitializes socket communication channels
func (w *Worker) initSocketChannels() {
	w.socketMu.Lock()
	defer w.socketMu.Unlock()
	
	// Increment socket manager ID to invalidate old managers
	w.socketManagerID++
	
	// Create new channels for this socket session
	w.sendCh = make(chan workerMessage, 10)
	w.recvCh = make(chan zmq4.Msg, 10)
	w.errorCh = make(chan error, 1)
	w.closeCh = make(chan struct{})
	w.socketDone = make(chan struct{})
}

// closeSocketChannels safely closes socket communication channels
func (w *Worker) closeSocketChannels() {
	w.socketMu.Lock()
	defer w.socketMu.Unlock()
	
	// Signal any running socket manager to stop
	select {
	case w.closeCh <- struct{}{}:
	default:
	}
	
	// Wait for socket manager to finish (with timeout to prevent deadlock)
	select {
	case <-w.socketDone:
		// Socket manager finished cleanly
	case <-time.After(1 * time.Second):
		// Timeout - socket manager may be stuck, continue anyway
		if w.options.LogErrors {
			log.Printf("MDP worker: timeout waiting for socket manager to finish")
		}
	}
}

// Start starts the worker
func (w *Worker) Start() error {
	running := w.queryState(queryIsRunning)
	if running != nil && running.(bool) {
		return fmt.Errorf("mdp: worker already running")
	}
	
	w.updateState(stateSetRunning, true)
	
	// Start worker loops
	go w.workerLoop()
	
	if w.options.LogInfo {
		log.Printf("MDP worker for service %s started", w.service)
	}
	
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() error {
	running := w.queryState(queryIsRunning)
	if running == nil || !running.(bool) {
		// Worker is not running, but we still need to clean up the state manager
		w.stopStateManager()
		return fmt.Errorf("mdp: worker not running")
	}
	
	// Send disconnect message
	connected := w.queryState(queryIsConnected)
	if connected != nil && connected.(bool) {
		w.sendToBroker(WorkerDisconnect, nil, nil)
	}
	
	w.updateState(stateSetRunning, false)
	
	// First, stop the worker loop
	w.cancel()
	close(w.stopCh)
	
	// Stop any running socket manager
	w.closeSocketChannels()
	
	// Close socket
	w.mu.Lock()
	if w.socket != nil {
		err := w.socket.Close()
		if err != nil {
			w.mu.Unlock()
			return fmt.Errorf("mdp: failed to close worker socket: %w", err)
		}
		w.socket = nil
	}
	w.mu.Unlock()
	
	// Stop state manager
	w.stopStateManager()
	
	if w.options.LogInfo {
		log.Printf("MDP worker for service %s stopped", w.service)
	}
	
	return nil
}

// stopStateManager stops the state manager goroutine safely
func (w *Worker) stopStateManager() {
	// Stop state manager and wait for it to finish (only once)
	if atomic.CompareAndSwapInt32(&w.stateStopped, 0, 1) {
		close(w.stateUpdateCh)
		select {
		case <-w.stateDone:
			// State manager stopped cleanly
		case <-time.After(1 * time.Second):
			// Timeout - state manager didn't stop, but continue anyway
			if w.options.LogErrors {
				log.Printf("MDP worker: timeout waiting for state manager to stop")
			}
		}
	}
}

// workerLoop is the main worker processing loop
func (w *Worker) workerLoop() {
	defer func() {
		// Ensure any running socket manager is stopped
		w.closeSocketChannels()
		
		// State manager will be stopped by Stop() method
	}()
	
	for {
		select {
		case <-w.stopCh:
			return
		default:
			// Ensure previous socket manager is stopped before starting new one
			w.closeSocketChannels()
			
			// Connect to broker (this will reinitialize channels)
			if !w.connectToBroker() {
				// Connection failed, wait before retrying (with cancellation check)
				select {
				case <-w.stopCh:
					return
				case <-time.After(w.options.ReconnectInterval):
					continue
				}
			}
			
			// Start socket manager goroutine with current session ID
			currentManagerID := w.getSocketManagerID()
			if w.options.LogInfo {
				log.Printf("MDP worker: starting socket manager session %d", currentManagerID)
			}
			go w.socketManager(currentManagerID)
			
			// Process messages (this will return on disconnect or error)
			if w.options.LogInfo {
				log.Printf("MDP worker: starting message processing loop")
			}
			w.processMessages()
			
			// Wait for socket manager to finish (with timeout)
			select {
			case <-w.socketDone:
				// Socket manager finished normally
			case <-time.After(2 * time.Second):
				// Timeout - force close and continue
				if w.options.LogErrors {
					log.Printf("MDP worker: socket manager did not finish within timeout")
				}
			}
			
			// Check if we should continue running
			running := w.queryState(queryIsRunning)
			if running == nil || !running.(bool) {
				return
			}
			
			// Wait before reconnecting (with cancellation check)
			select {
			case <-w.stopCh:
				return
			case <-time.After(w.options.ReconnectInterval):
				// Continue with reconnection
			}
		}
	}
}

// stateManager manages all state and statistics in a single goroutine (thread-safe)
func (w *Worker) stateManager() {
	defer close(w.stateDone)
	
	// Local state variables (only accessed by this goroutine)
	var (
		connected      bool
		running        bool
		expectReply    bool
		replyTo        []byte
		liveness       int
		heartbeatAt    time.Time
		socketReady    bool
		totalRequests  uint64
		totalReplies   uint64
		totalErrors    uint64
		reconnects     uint64
	)
	
	// Initialize state
	running = false  // Worker starts as not running until Start() is called
	liveness = w.options.HeartbeatLiveness
	heartbeatAt = time.Now().Add(w.options.HeartbeatInterval)
	
	for {
		select {
		case update, ok := <-w.stateUpdateCh:
			if !ok {
				// Channel closed, exit
				return
			}
			
			switch update.updateType {
			case stateConnected:
				connected = true
				liveness = w.options.HeartbeatLiveness
				heartbeatAt = time.Now().Add(w.options.HeartbeatInterval)
			case stateDisconnected:
				connected = false
				socketReady = false
				expectReply = false
				replyTo = nil
			case stateExpectReply:
				expectReply = true
				if addr, ok := update.data.([]byte); ok {
					replyTo = addr
				}
			case stateReplyComplete:
				expectReply = false
				replyTo = nil
			case stateSetLiveness:
				if val, ok := update.data.(int); ok {
					liveness = val
				}
			case stateDecreaseLiveness:
				liveness--
			case stateSetHeartbeatTime:
				if val, ok := update.data.(time.Time); ok {
					heartbeatAt = val
				}
			case stateSetRunning:
				if val, ok := update.data.(bool); ok {
					running = val
				}
			case stateSetSocketReady:
				if val, ok := update.data.(bool); ok {
					socketReady = val
				}
			case stateSetSocketClosed:
				socketReady = false
			}
			
		case stats := <-w.statsUpdateCh:
			switch stats.updateType {
			case statsIncRequests:
				totalRequests++
			case statsIncReplies:
				totalReplies++
			case statsIncErrors:
				totalErrors++
			case statsIncReconnects:
				reconnects++
			}
			
		case query := <-w.queryCh:
			switch query.queryType {
			case queryIsConnected:
				query.responseCh <- connected
			case queryIsRunning:
				query.responseCh <- running
			case queryGetExpectReply:
				query.responseCh <- expectReply
			case queryGetReplyTo:
				query.responseCh <- replyTo
			case queryGetHeartbeatTime:
				query.responseCh <- heartbeatAt
			case queryGetLiveness:
				query.responseCh <- liveness
			case queryIsSocketReady:
				query.responseCh <- socketReady
			case queryGetStats:
				stats := map[string]interface{}{
					"service":         string(w.service),
					"total_requests":  totalRequests,
					"total_replies":   totalReplies,
					"total_errors":    totalErrors,
					"reconnects":      reconnects,
					"connected":       connected,
					"running":         running,
					"liveness":        liveness,
				}
				query.responseCh <- stats
			}
		}
	}
}

// getSocketManagerID returns the current socket manager ID
func (w *Worker) getSocketManagerID() uint64 {
	w.socketMu.RLock()
	defer w.socketMu.RUnlock()
	return w.socketManagerID
}

// Helper functions for channel-based updates
func (w *Worker) updateState(updateType stateUpdateType, data interface{}) {
	select {
	case w.stateUpdateCh <- stateUpdate{updateType: updateType, data: data}:
	case <-w.stopCh:
	default:
		// Channel full, drop update
	}
}

func (w *Worker) updateStats(statsType statsUpdateType) {
	select {
	case w.statsUpdateCh <- statsUpdate{updateType: statsType}:
	case <-w.stopCh:
	default:
		// Channel full, drop update
	}
}

func (w *Worker) queryState(queryType queryType) interface{} {
	responseCh := make(chan interface{}, 1)
	select {
	case w.queryCh <- queryRequest{queryType: queryType, responseCh: responseCh}:
		select {
		case result := <-responseCh:
			return result
		case <-w.stopCh:
			return nil
		case <-time.After(100 * time.Millisecond): // Add timeout to prevent deadlocks
			return nil
		}
	case <-w.stopCh:
		return nil
	default:
		return nil // Query channel full
	}
}

// socketManager manages all socket operations in a single goroutine (thread-safe)
func (w *Worker) socketManager(sessionID uint64) {
	defer close(w.socketDone)
	
	if w.options.LogInfo {
		log.Printf("MDP worker: socketManager session %d started", sessionID)
	}
	
	// Validate session ID to ensure this is the current socket manager
	if sessionID != w.getSocketManagerID() {
		if w.options.LogInfo {
			log.Printf("MDP worker: socketManager session %d is obsolete, exiting", sessionID)
		}
		return
	}
	
	// Use a single goroutine for all socket operations to ensure thread safety
	for {
		// Validate session ID on each iteration
		if sessionID != w.getSocketManagerID() {
			if w.options.LogInfo {
				log.Printf("MDP worker: socketManager session %d invalidated, exiting", sessionID)
			}
			return
		}
		
		// Check for send operations first (higher priority)
		select {
		case <-w.closeCh:
			// Close socket and exit
			w.mu.Lock()
			if w.socket != nil {
				w.socket.Close()
				w.socket = nil
			}
			w.mu.Unlock()
			return
			
		case msg := <-w.sendCh:
			// Validate session before sending
			if sessionID != w.getSocketManagerID() {
				if w.options.LogInfo {
					log.Printf("MDP worker: socketManager session %d invalidated during send", sessionID)
				}
				return
			}
			
			// Send message to broker
			if w.options.LogInfo && msg.command == WorkerReply {
				log.Printf("MDP worker: socketManager received REPLY from sendCh")
			}
			
			w.mu.RLock()
			socket := w.socket
			w.mu.RUnlock()
			
			if socket != nil {
				if w.options.LogInfo && msg.command == WorkerReply {
					log.Printf("MDP worker: socketManager calling doSendToBroker for REPLY")
				}
				w.doSendToBroker(socket, msg.command, msg.clientAddr, msg.body)
			} else {
				if w.options.LogErrors {
					log.Printf("MDP worker: socketManager cannot send %s - socket is nil", msg.command)
				}
			}
			
		default:
			// Validate session before receiving
			if sessionID != w.getSocketManagerID() {
				if w.options.LogInfo {
					log.Printf("MDP worker: socketManager session %d invalidated during receive", sessionID)
				}
				return
			}
			
			// No send operations pending, try to receive
			w.mu.RLock()
			socket := w.socket
			w.mu.RUnlock()
			
			if socket != nil {
				if w.options.LogInfo {
					log.Printf("MDP worker: socket manager attempting socket.Recv() call")
				}
				
				// Blocking recv call - simpler and more reliable than async pattern
				msg, err := socket.Recv()
				
				if w.options.LogInfo {
					if err != nil {
						log.Printf("MDP worker: socket.Recv() returned error: %v", err)
					} else {
						log.Printf("MDP worker: socket.Recv() returned %d frames", len(msg.Frames))
					}
				}
				
				// Process the result immediately
				if err != nil {
					if w.options.LogInfo {
						log.Printf("MDP worker: socket manager forwarding error to errorCh: %v", err)
					}
					select {
					case w.errorCh <- err:
						if w.options.LogInfo {
							log.Printf("MDP worker: error sent to errorCh")
						}
					case <-w.closeCh:
						if w.options.LogInfo {
							log.Printf("MDP worker: socket manager cancelled during error handling")
						}
						return
					default:
						if w.options.LogInfo {
							log.Printf("MDP worker: errorCh full, dropping error")
						}
					}
				} else {
					if w.options.LogInfo {
						log.Printf("MDP worker: socket manager forwarding message to recvCh")
					}
					select {
					case w.recvCh <- msg:
						if w.options.LogInfo {
							log.Printf("MDP worker: message sent to recvCh successfully")
						}
					case <-w.closeCh:
						if w.options.LogInfo {
							log.Printf("MDP worker: socket manager cancelled during message forwarding")
						}
						return
					}
				}
			} else {
				// No socket, wait a bit
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
}

// connectToBroker connects to the broker and sends READY
func (w *Worker) connectToBroker() bool {
	connected := w.queryState(queryIsConnected)
	if connected != nil && connected.(bool) {
		return true
	}
	
	// Close existing socket if any
	w.mu.Lock()
	if w.socket != nil {
		w.socket.Close()
		w.socket = nil
	}
	w.mu.Unlock()
	
	// Initialize new socket channels for this connection session
	w.initSocketChannels()
	
	// CRITICAL: Generate unique identity for ROUTER->DEALER routing
	// This identity will be used by the broker to route messages back to this worker
	workerIdentity := fmt.Sprintf("worker-%s-%d", w.service, time.Now().UnixNano())
	workerIdentityBytes := []byte(workerIdentity)
	
	// Create new DEALER socket with identity and security if configured
	var socket zmq4.Socket
	if w.options.Security != nil {
		if w.options.LogInfo {
			log.Printf("MDP worker: creating DEALER socket with %s security and identity %s", w.options.Security.Type(), workerIdentity)
		}
		socket = zmq4.NewDealer(w.ctx, zmq4.WithID(zmq4.SocketIdentity(workerIdentityBytes)), zmq4.WithSecurity(w.options.Security))
	} else {
		if w.options.LogInfo {
			log.Printf("MDP worker: creating DEALER socket without security with identity %s", workerIdentity)
		}
		socket = zmq4.NewDealer(w.ctx, zmq4.WithID(zmq4.SocketIdentity(workerIdentityBytes)))
	}
	
	if w.options.LogInfo {
		log.Printf("MDP worker: attempting to dial broker at %s", w.brokerEndpoint)
	}
	
	err := socket.Dial(w.brokerEndpoint)
	if err != nil {
		if w.options.LogErrors {
			log.Printf("MDP worker: failed to connect to broker %s: %v", w.brokerEndpoint, err)
		}
		return false
	}
	
	if w.options.LogInfo {
		log.Printf("MDP worker: socket dial successful, setting socket reference")
	}
	
	w.mu.Lock()
	w.socket = socket
	w.mu.Unlock()
	
	if w.options.LogInfo {
		log.Printf("MDP worker: socket reference set, socket is ready for communication")
	}
	
	w.updateState(stateConnected, nil)
	w.updateStats(statsIncReconnects)
	
	// Validate CURVE security is ready for MESSAGE transmission if using CURVE
	if w.options.Security != nil {
		if err := w.validateSecurityReadiness(); err != nil {
			if w.options.LogErrors {
				log.Printf("MDP worker: security validation failed after connection: %v", err)
			}
			// Close the socket and fail the connection
			w.mu.Lock()
			if w.socket != nil {
				w.socket.Close()
				w.socket = nil
			}
			w.mu.Unlock()
			w.updateState(stateSetSocketClosed, nil)
			return false
		}
	}
	
	// Mark socket as ready for operations
	if w.options.LogInfo {
		log.Printf("MDP worker: marking socket as ready for operations")
	}
	w.updateState(stateSetSocketReady, true)
	
	// Wait briefly to ensure state update is processed
	time.Sleep(10 * time.Millisecond)
	
	// Send READY message
	if w.options.LogInfo {
		log.Printf("MDP worker: sending READY message for service %s", w.service)
	}
	w.sendToBroker(WorkerReady, nil, []byte(w.service))
	
	if w.options.LogInfo {
		log.Printf("MDP worker: connected to broker %s for service %s", w.brokerEndpoint, w.service)
	}
	
	return true
}

// validateSecurityReadiness validates that the security mechanism is ready for MESSAGE operations
func (w *Worker) validateSecurityReadiness() error {
	if w.options.Security == nil {
		return nil // No security, nothing to validate
	}
	
	// Check if this is a CURVE security mechanism
	if curveSec, ok := w.options.Security.(interface {
		ValidateMessageReadiness() error
	}); ok {
		// Give CURVE a moment to finalize handshake state
		time.Sleep(10 * time.Millisecond)
		
		return curveSec.ValidateMessageReadiness()
	}
	
	// For other security mechanisms, just check basic readiness
	if readySec, ok := w.options.Security.(interface {
		IsReady() bool
	}); ok {
		if !readySec.IsReady() {
			return fmt.Errorf("security mechanism not ready")
		}
	}
	
	return nil
}

// validateConnectionState performs comprehensive validation of connection state
func (w *Worker) validateConnectionState() error {
	// Check basic connection state
	connected := w.queryState(queryIsConnected)
	if connected == nil || !connected.(bool) {
		return fmt.Errorf("worker not connected to broker")
	}
	
	// Check socket readiness
	socketReady := w.queryState(queryIsSocketReady)
	if socketReady == nil || !socketReady.(bool) {
		return fmt.Errorf("socket not ready for operations")
	}
	
	// Check if we have a valid socket
	w.mu.RLock()
	socket := w.socket
	w.mu.RUnlock()
	
	if socket == nil {
		return fmt.Errorf("socket is nil")
	}
	
	// Check socket manager is running
	currentManagerID := w.getSocketManagerID()
	if currentManagerID == 0 {
		return fmt.Errorf("socket manager not initialized")
	}
	
	// Validate security state if using security
	if w.options.Security != nil {
		if err := w.validateSecurityReadiness(); err != nil {
			return fmt.Errorf("security validation failed: %w", err)
		}
	}
	
	// Check liveness
	liveness := w.queryState(queryGetLiveness)
	if liveness != nil && liveness.(int) <= 0 {
		return fmt.Errorf("broker liveness exhausted")
	}
	
	return nil
}

// isHealthy returns true if the worker connection is healthy
func (w *Worker) isHealthy() bool {
	return w.validateConnectionState() == nil
}

// processMessages processes messages from the broker via channels (thread-safe)
func (w *Worker) processMessages() {
	ticker := time.NewTicker(100 * time.Millisecond) // Check timeout frequently
	defer ticker.Stop()
	
	healthCheckCounter := 0
	const healthCheckInterval = 50 // Check health every 5 seconds (50 * 100ms)
	
	for {
		select {
		case <-w.stopCh:
			// Signal socket manager to close (non-blocking)
			select {
			case w.closeCh <- struct{}{}:
			default:
			}
			return
			
		case <-ticker.C:
			healthCheckCounter++
			
			// Periodic health check
			if healthCheckCounter >= healthCheckInterval {
				healthCheckCounter = 0
				if err := w.validateConnectionState(); err != nil {
					if w.options.LogErrors {
						log.Printf("MDP worker: connection health check failed: %v", err)
					}
					w.disconnect()
					return
				}
			}
			
			// Check if we need to send heartbeat
			heartbeatTime := w.queryState(queryGetHeartbeatTime)
			if heartbeatTime != nil && time.Now().After(heartbeatTime.(time.Time)) {
				w.sendHeartbeat()
			}
			
			// Check for broker timeout
			liveness := w.queryState(queryGetLiveness)
			if liveness != nil && liveness.(int) <= 0 {
				if w.options.LogErrors {
					log.Printf("MDP worker: broker appears to be offline, reconnecting...")
				}
				w.disconnect()
				return
			}
			
		case msg := <-w.recvCh:
			// Validate connection state before processing message
			if err := w.validateConnectionState(); err != nil {
				if w.options.LogErrors {
					log.Printf("MDP worker: invalid connection state when receiving message: %v", err)
				}
				w.disconnect()
				return
			}
			
			if w.options.LogInfo {
				log.Printf("MDP worker: received %d frames from broker", len(msg.Frames))
			}
			w.processMessage(msg)
			
		case err := <-w.errorCh:
			if w.options.LogErrors {
				log.Printf("MDP worker: socket error: %v", err)
			}
			// Socket error - reconnect
			w.disconnect()
			return
			
		default:
			// Non-blocking continue to check other cases
			time.Sleep(10 * time.Millisecond)
		}
	}
}


// processMessage processes a single message from the broker
func (w *Worker) processMessage(msg zmq4.Msg) {
	frames := msg.Frames
	
	if w.options.LogInfo {
		log.Printf("MDP worker: received %d frames from broker:", len(frames))
		for i, frame := range frames {
			if len(frame) > 0 {
				log.Printf("  frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	// Reset liveness - broker is alive
	w.updateState(stateSetLiveness, w.options.HeartbeatLiveness)
	
	if w.options.LogInfo {
		log.Printf("MDP worker: about to parse worker message with %d frames:", len(frames))
		for i, frame := range frames {
			if len(frame) > 0 {
				log.Printf("  parse_frame[%d]: %q (len=%d, bytes=%v)", i, string(frame), len(frame), frame)
			} else {
				log.Printf("  parse_frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	workerMsg, err := ParseWorkerMessage(frames)
	if err != nil {
		if w.options.LogErrors {
			log.Printf("MDP worker: invalid message from broker: %v", err)
		}
		return
	}
	
	if w.options.LogInfo {
		log.Printf("MDP worker: received command 0x%02x from broker", []byte(workerMsg.Command)[0])
		log.Printf("MDP worker: command string: %q, comparing to WorkerRequest %q", workerMsg.Command, WorkerRequest)
	}
	
	switch workerMsg.Command {
	case WorkerRequest:
		if w.options.LogInfo {
			log.Printf("MDP worker: processing REQUEST command")
		}
		w.processRequest(workerMsg)
		
	case WorkerHeartbeat:
		// Nothing to do - liveness already reset
		
	case WorkerDisconnect:
		if w.options.LogInfo {
			log.Printf("MDP worker: received disconnect from broker")
		}
		w.disconnect()
		
	default:
		if w.options.LogErrors {
			log.Printf("MDP worker: unknown command from broker: 0x%02x", []byte(workerMsg.Command)[0])
		}
	}
}

// processRequest processes a request from a client
func (w *Worker) processRequest(workerMsg *Message) {
	if w.options.LogInfo {
		log.Printf("MDP worker: ← received REQUEST from client %x (len=%d)", workerMsg.ClientAddr, len(workerMsg.Body))
		log.Printf("MDP worker: request body: %q", string(workerMsg.Body))
	}
	
	expectReply := w.queryState(queryGetExpectReply)
	if expectReply != nil && expectReply.(bool) {
		if w.options.LogErrors {
			log.Printf("MDP worker: received request while expecting reply")
		}
		return
	}
	
	w.updateState(stateExpectReply, workerMsg.ClientAddr)
	w.updateStats(statsIncRequests)
	
	// Process the request
	reply, err := w.handler(workerMsg.Body)
	if err != nil {
		if w.options.LogErrors {
			log.Printf("MDP worker: request handler error: %v", err)
		}
		
		w.updateStats(statsIncErrors)
		
		// Send error response
		reply = []byte(fmt.Sprintf("Error: %v", err))
	}
	
	if w.options.LogInfo {
		log.Printf("MDP worker: processed request, reply ready (len=%d)", len(reply))
	}
	
	// Send reply
	w.sendReply(reply)
}

// sendReply sends a reply to the client
func (w *Worker) sendReply(reply []byte) {
	expectReply := w.queryState(queryGetExpectReply)
	if expectReply == nil || !expectReply.(bool) {
		if w.options.LogErrors {
			log.Printf("MDP worker: attempt to send reply when not expected")
		}
		return
	}
	
	replyTo := w.queryState(queryGetReplyTo)
	if replyTo == nil {
		if w.options.LogErrors {
			log.Printf("MDP worker: no reply address available")
		}
		return
	}
	
	replyAddr := replyTo.([]byte)
	if w.options.LogInfo {
		log.Printf("MDP worker: → sending reply to client %x (len=%d)", replyAddr, len(reply))
	}
	
	w.sendToBroker(WorkerReply, replyAddr, reply)
	
	w.updateState(stateReplyComplete, nil)
	w.updateStats(statsIncReplies)
}

// sendToBroker sends a message to the broker via channel (thread-safe)
func (w *Worker) sendToBroker(command string, clientAddr []byte, body []byte) {
	// Check if socket is ready for message transmission
	socketReady := w.queryState(queryIsSocketReady)
	if socketReady == nil || !socketReady.(bool) {
		if w.options.LogErrors {
			log.Printf("MDP worker: cannot send %s - socket not ready", command)
		}
		w.updateState(stateDecreaseLiveness, nil)
		return
	}
	
	if w.options.LogInfo && command == WorkerReply {
		log.Printf("MDP worker: → sending REPLY to broker for client %x", clientAddr)
	}
	
	msg := workerMessage{
		command:    command,
		clientAddr: clientAddr,
		body:       body,
	}
	
	if w.options.LogInfo && command == WorkerReply {
		log.Printf("MDP worker: attempting to queue REPLY message to sendCh")
	}
	
	select {
	case w.sendCh <- msg:
		// Message queued successfully
		if w.options.LogInfo && command == WorkerReply {
			log.Printf("MDP worker: ✓ queued REPLY message to sendCh")
		}
	case <-w.stopCh:
		// Worker is stopping
		return
	default:
		// Channel is full, log error
		if w.options.LogErrors {
			log.Printf("MDP worker: send channel full, dropping %s message", command)
		}
		w.updateState(stateDecreaseLiveness, nil)
	}
}

// doSendToBroker performs the actual socket send operation (called only by socketManager)
func (w *Worker) doSendToBroker(socket zmq4.Socket, command string, clientAddr []byte, body []byte) {
	msg := NewWorkerMessage(command, clientAddr, body)
	
	// For READY messages, the service should be in the Service field, not Body
	if command == WorkerReady {
		msg.Service = ServiceName(body)
		msg.Body = nil
	}
	
	frames := msg.FormatWorkerMessage()
	
	if w.options.LogInfo && command == WorkerReply {
		log.Printf("MDP worker: formatting REPLY message with %d frames:", len(frames))
		for i, frame := range frames {
			if len(frame) > 0 {
				log.Printf("  frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	zmqMsg := zmq4.NewMsgFrom(frames...)
	
	err := socket.Send(zmqMsg)
	if err != nil {
		if w.options.LogErrors {
			log.Printf("MDP worker: failed to send %s to broker: %v", command, err)
		}
		w.updateState(stateDecreaseLiveness, nil)
	} else if w.options.LogInfo && command == WorkerReply {
		log.Printf("MDP worker: ✓ sent REPLY to broker")
	}
}

// sendHeartbeat sends a heartbeat to the broker
func (w *Worker) sendHeartbeat() {
	connected := w.queryState(queryIsConnected)
	if connected == nil || !connected.(bool) {
		return
	}
	
	w.sendToBroker(WorkerHeartbeat, nil, nil)
	w.updateState(stateSetHeartbeatTime, time.Now().Add(w.options.HeartbeatInterval))
	// Note: Heartbeat should EXTEND liveness, not decrease it
	// The liveness is reset to full when we receive any message from broker
}

// disconnect disconnects from the broker via channel coordination (thread-safe)
func (w *Worker) disconnect() {
	connected := w.queryState(queryIsConnected)
	if connected == nil || !connected.(bool) {
		return
	}
	
	// Mark socket as not ready and disconnected
	w.updateState(stateSetSocketClosed, nil)
	w.updateState(stateDisconnected, nil)
	
	// Signal socket manager to close socket and exit (non-blocking)
	select {
	case w.closeCh <- struct{}{}:
		// Successfully signaled close
	default:
		// Channel already closed, full, or no receiver - that's ok
	}
	
	if w.options.LogInfo {
		log.Printf("MDP worker: disconnected from broker")
	}
	
	// Wait before reconnecting
	time.Sleep(w.options.ReconnectInterval)
}

// GetStats returns worker statistics
func (w *Worker) GetStats() map[string]interface{} {
	stats := w.queryState(queryGetStats)
	if stats == nil {
		// Fallback if query fails
		return map[string]interface{}{
			"service": string(w.service),
			"error":   "unable to query stats",
		}
	}
	return stats.(map[string]interface{})
}

// SetHandler updates the request handler
func (w *Worker) SetHandler(handler RequestHandler) error {
	if handler == nil {
		return fmt.Errorf("mdp: request handler cannot be nil")
	}
	
	w.mu.Lock()
	defer w.mu.Unlock()
	
	w.handler = handler
	return nil
}

// GetService returns the service name this worker serves
func (w *Worker) GetService() ServiceName {
	return w.service
}

// IsConnected returns whether the worker is connected to the broker
func (w *Worker) IsConnected() bool {
	connected := w.queryState(queryIsConnected)
	if connected == nil {
		return false
	}
	return connected.(bool)
}

// IsRunning returns whether the worker is running
func (w *Worker) IsRunning() bool {
	running := w.queryState(queryIsRunning)
	if running == nil {
		return false
	}
	return running.(bool)
}