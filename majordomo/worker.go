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
	
	// Create new DEALER socket with security if configured
	var socket zmq4.Socket
	if w.options.Security != nil {
		socket = zmq4.NewDealer(w.ctx, zmq4.WithSecurity(w.options.Security))
	} else {
		socket = zmq4.NewDealer(w.ctx)
	}
	
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
	
	// Create a single message channel for the socket reader
	msgCh := make(chan zmq4.Msg, 10)
	errCh := make(chan error, 1)
	
	// Start single reader goroutine
	go func() {
		for {
			select {
			case <-w.stopCh:
				return
			default:
				msg, err := w.socket.Recv()
				if err != nil {
					select {
					case errCh <- err:
					case <-w.stopCh:
						return
					}
				} else {
					if w.options.LogInfo {
						log.Printf("MDP worker: socket received %d frames", len(msg.Frames))
					}
					select {
					case msgCh <- msg:
					case <-w.stopCh:
						return
					}
				}
			}
		}
	}()
	
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
			
		case msg := <-msgCh:
			w.processMessage(msg)
			
		case err := <-errCh:
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
	
	if w.options.LogInfo {
		log.Printf("MDP worker: received command 0x%02x from broker", []byte(workerMsg.Command)[0])
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
			log.Printf("MDP worker: unknown command from broker: 0x%02x", []byte(workerMsg.Command)[0])
		}
	}
}

// processRequest processes a request from a client
func (w *Worker) processRequest(workerMsg *Message) {
	if w.options.LogInfo {
		log.Printf("MDP worker: ← received REQUEST from client %x (len=%d)", workerMsg.ClientAddr, len(workerMsg.Body))
	}
	
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
	
	if w.options.LogInfo {
		log.Printf("MDP worker: processed request, reply ready (len=%d)", len(reply))
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
	
	if w.options.LogInfo {
		log.Printf("MDP worker: → sending reply to client %x (len=%d)", w.replyTo, len(reply))
	}
	
	w.sendToBroker(WorkerReply, w.replyTo, reply)
	
	w.expectReply = false
	w.replyTo = nil
	w.totalReplies++
}

// sendToBroker sends a message to the broker
func (w *Worker) sendToBroker(command string, clientAddr []byte, body []byte) {
	if w.options.LogInfo && command == WorkerReply {
		log.Printf("MDP worker: → sending %s to broker for client %x", command, clientAddr)
	}
	
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
	} else if w.options.LogInfo && command == WorkerReply {
		log.Printf("MDP worker: ✓ sent %s to broker", command)
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