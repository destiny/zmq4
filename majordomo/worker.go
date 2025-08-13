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

// RequestHandler is a function that processes worker requests
type RequestHandler func(request []byte) ([]byte, error)

// WorkerOptions configures MDP worker behavior
type WorkerOptions struct {
	HeartbeatLiveness int           // Heartbeat liveness factor
	HeartbeatInterval time.Duration // Heartbeat interval
	ReconnectInterval time.Duration // Reconnection interval
	LogErrors         bool          // Whether to log errors
	LogInfo           bool          // Whether to log info messages
}

// DefaultWorkerOptions returns default worker options
func DefaultWorkerOptions() *WorkerOptions {
	return &WorkerOptions{
		HeartbeatLiveness: DefaultHeartbeatLiveness,
		HeartbeatInterval: DefaultHeartbeatInterval,
		ReconnectInterval: 2500 * time.Millisecond,
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
	
	return &Worker{
		service:        service,
		brokerEndpoint: brokerEndpoint,
		options:        options,
		handler:        handler,
		ctx:            ctx,
		cancel:         cancel,
		liveness:       options.HeartbeatLiveness,
		stopCh:         make(chan struct{}),
		heartbeatCh:    make(chan struct{}, 1),
	}, nil
}

// Start starts the worker
func (w *Worker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if w.running {
		return fmt.Errorf("mdp: worker already running")
	}
	
	w.running = true
	
	// Start worker loops
	go w.workerLoop()
	
	if w.options.LogInfo {
		log.Printf("MDP worker for service %s started", w.service)
	}
	
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if !w.running {
		return fmt.Errorf("mdp: worker not running")
	}
	
	w.running = false
	
	// Send disconnect message
	if w.connected {
		w.sendToBroker(WorkerDisconnect, nil, nil)
	}
	
	w.cancel()
	close(w.stopCh)
	
	if w.socket != nil {
		err := w.socket.Close()
		if err != nil {
			return fmt.Errorf("mdp: failed to close worker socket: %w", err)
		}
	}
	
	if w.options.LogInfo {
		log.Printf("MDP worker for service %s stopped", w.service)
	}
	
	return nil
}

// workerLoop is the main worker processing loop
func (w *Worker) workerLoop() {
	for {
		select {
		case <-w.stopCh:
			return
		default:
			w.connectToBroker()
			w.processMessages()
		}
	}
}

// connectToBroker connects to the broker and sends READY
func (w *Worker) connectToBroker() {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if w.connected {
		return
	}
	
	// Close existing socket if any
	if w.socket != nil {
		w.socket.Close()
	}
	
	// Create new DEALER socket
	socket := zmq4.NewDealer(w.ctx)
	
	err := socket.Dial(w.brokerEndpoint)
	if err != nil {
		if w.options.LogErrors {
			log.Printf("MDP worker: failed to connect to broker %s: %v", w.brokerEndpoint, err)
		}
		time.Sleep(w.options.ReconnectInterval)
		return
	}
	
	w.socket = socket
	w.liveness = w.options.HeartbeatLiveness
	w.heartbeatAt = time.Now().Add(w.options.HeartbeatInterval)
	
	// Send READY message
	w.sendToBroker(WorkerReady, nil, []byte(w.service))
	w.connected = true
	w.reconnects++
	
	if w.options.LogInfo {
		log.Printf("MDP worker: connected to broker %s for service %s", w.brokerEndpoint, w.service)
	}
}

// processMessages processes messages from the broker
func (w *Worker) processMessages() {
	ticker := time.NewTicker(100 * time.Millisecond) // Check timeout frequently
	defer ticker.Stop()
	
	for {
		select {
		case <-w.stopCh:
			return
			
		case <-ticker.C:
			// Check if we need to send heartbeat
			if time.Now().After(w.heartbeatAt) {
				w.sendHeartbeat()
			}
			
			// Check for broker timeout
			if w.liveness <= 0 {
				if w.options.LogErrors {
					log.Printf("MDP worker: broker appears to be offline, reconnecting...")
				}
				w.disconnect()
				return
			}
			
		default:
			// Try to receive message with short timeout
			ctx, cancel := context.WithTimeout(w.ctx, 100*time.Millisecond)
			
			msg, err := w.recvMessage(ctx)
			cancel()
			
			if err != nil {
				continue // Timeout or other error, continue loop
			}
			
			w.processMessage(msg)
		}
	}
}

// recvMessage receives a message from the broker
func (w *Worker) recvMessage(ctx context.Context) (zmq4.Msg, error) {
	// Use a goroutine to make Recv cancellable
	msgCh := make(chan zmq4.Msg, 1)
	errCh := make(chan error, 1)
	
	go func() {
		msg, err := w.socket.Recv()
		if err != nil {
			errCh <- err
		} else {
			msgCh <- msg
		}
	}()
	
	select {
	case msg := <-msgCh:
		return msg, nil
	case err := <-errCh:
		return zmq4.Msg{}, err
	case <-ctx.Done():
		return zmq4.Msg{}, ctx.Err()
	}
}

// processMessage processes a single message from the broker
func (w *Worker) processMessage(msg zmq4.Msg) {
	frames := msg.Frames
	
	// Reset liveness - broker is alive
	w.mu.Lock()
	w.liveness = w.options.HeartbeatLiveness
	w.mu.Unlock()
	
	workerMsg, err := ParseWorkerMessage(frames)
	if err != nil {
		if w.options.LogErrors {
			log.Printf("MDP worker: invalid message from broker: %v", err)
		}
		return
	}
	
	switch workerMsg.Command {
	case WorkerRequest:
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
			log.Printf("MDP worker: unknown command from broker: %s", workerMsg.Command)
		}
	}
}

// processRequest processes a request from a client
func (w *Worker) processRequest(workerMsg *Message) {
	if w.expectReply {
		if w.options.LogErrors {
			log.Printf("MDP worker: received request while expecting reply")
		}
		return
	}
	
	w.mu.Lock()
	w.expectReply = true
	w.replyTo = workerMsg.ClientAddr
	w.totalRequests++
	w.mu.Unlock()
	
	// Process the request
	reply, err := w.handler(workerMsg.Body)
	if err != nil {
		if w.options.LogErrors {
			log.Printf("MDP worker: request handler error: %v", err)
		}
		
		w.mu.Lock()
		w.totalErrors++
		w.mu.Unlock()
		
		// Send error response
		reply = []byte(fmt.Sprintf("Error: %v", err))
	}
	
	// Send reply
	w.sendReply(reply)
}

// sendReply sends a reply to the client
func (w *Worker) sendReply(reply []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if !w.expectReply {
		if w.options.LogErrors {
			log.Printf("MDP worker: attempt to send reply when not expected")
		}
		return
	}
	
	w.sendToBroker(WorkerReply, w.replyTo, reply)
	
	w.expectReply = false
	w.replyTo = nil
	w.totalReplies++
}

// sendToBroker sends a message to the broker
func (w *Worker) sendToBroker(command string, clientAddr []byte, body []byte) {
	msg := NewWorkerMessage(command, clientAddr, body)
	
	// For READY messages, the service should be in the Service field, not Body
	if command == WorkerReady {
		msg.Service = ServiceName(body)
		msg.Body = nil
	}
	
	frames := msg.FormatWorkerMessage()
	
	zmqMsg := zmq4.NewMsgFrom(frames...)
	
	err := w.socket.Send(zmqMsg)
	if err != nil {
		if w.options.LogErrors {
			log.Printf("MDP worker: failed to send %s to broker: %v", command, err)
		}
		w.liveness--
	}
}

// sendHeartbeat sends a heartbeat to the broker
func (w *Worker) sendHeartbeat() {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if !w.connected {
		return
	}
	
	w.sendToBroker(WorkerHeartbeat, nil, nil)
	w.heartbeatAt = time.Now().Add(w.options.HeartbeatInterval)
	
	// Decrease liveness
	w.liveness--
}

// disconnect disconnects from the broker
func (w *Worker) disconnect() {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if !w.connected {
		return
	}
	
	w.connected = false
	w.expectReply = false
	w.replyTo = nil
	
	if w.socket != nil {
		w.socket.Close()
		w.socket = nil
	}
	
	if w.options.LogInfo {
		log.Printf("MDP worker: disconnected from broker")
	}
	
	// Wait before reconnecting
	time.Sleep(w.options.ReconnectInterval)
}

// GetStats returns worker statistics
func (w *Worker) GetStats() map[string]interface{} {
	w.mu.RLock()
	defer w.mu.RUnlock()
	
	return map[string]interface{}{
		"service":         string(w.service),
		"total_requests":  w.totalRequests,
		"total_replies":   w.totalReplies,
		"total_errors":    w.totalErrors,
		"reconnects":      w.reconnects,
		"connected":       w.connected,
		"running":         w.running,
		"liveness":        w.liveness,
	}
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
	w.mu.RLock()
	defer w.mu.RUnlock()
	
	return w.connected
}

// IsRunning returns whether the worker is running
func (w *Worker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	
	return w.running
}