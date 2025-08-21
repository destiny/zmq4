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

	"github.com/destiny/zmq4/v25"
)

// ClientOptions configures MDP client behavior
type ClientOptions struct {
	Timeout   time.Duration // Request timeout
	Retries   int           // Number of retries
	Security  zmq4.Security // Security mechanism (nil for no security)
	LogLevel  zmq4.LogLevel // Logging level (replaces LogErrors)
	
	// Deprecated: Use LogLevel instead
	LogErrors bool          // Whether to log errors
}

// DefaultClientOptions returns default client options
func DefaultClientOptions() *ClientOptions {
	return &ClientOptions{
		Timeout:   30 * time.Second,
		Retries:   3,
		Security:  nil,
		LogLevel:  zmq4.LogLevelWarn, // Default to WARN level
		// Backward compatibility
		LogErrors: true,
	}
}

// Client implements the MDP client
type Client struct {
	// Configuration
	brokerEndpoint string
	options        *ClientOptions
	
	// Networking
	socket zmq4.Socket
	ctx    context.Context
	cancel context.CancelFunc
	
	// Logging
	logger  *zmq4.Logger
	
	// State
	mu      sync.RWMutex
	running bool
	
	// Statistics
	totalRequests uint64
	totalReplies  uint64
	totalErrors   uint64
}

// NewClient creates a new MDP client
func NewClient(brokerEndpoint string, options *ClientOptions) *Client {
	if options == nil {
		options = DefaultClientOptions()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	// Initialize logger based on options
	var logger *zmq4.Logger
	if options.LogLevel != 0 {
		// Use new LogLevel
		logger = zmq4.NewLogger(options.LogLevel)
	} else {
		// Backward compatibility with boolean flags
		if options.LogErrors {
			logger = zmq4.NewLogger(zmq4.LogLevelError)
		} else {
			logger = zmq4.DevNullLogger
		}
	}
	
	return &Client{
		brokerEndpoint: brokerEndpoint,
		options:        options,
		logger:         logger,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Connect connects the client to the broker
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.running {
		return fmt.Errorf("mdp: client already connected")
	}
	
	// Create REQ socket for synchronous client with security if configured
	var socket zmq4.Socket
	if c.options.Security != nil {
		if c.options.LogErrors {
			log.Printf("MDP client: creating REQ socket with %s security", c.options.Security.Type())
		}
		socket = zmq4.NewReq(c.ctx, zmq4.WithSecurity(c.options.Security))
	} else {
		if c.options.LogErrors {
			log.Printf("MDP client: creating REQ socket without security")
		}
		socket = zmq4.NewReq(c.ctx)
	}
	
	if c.options.LogErrors {
		log.Printf("MDP client: attempting to dial broker at %s", c.brokerEndpoint)
	}
	
	err := socket.Dial(c.brokerEndpoint)
	if err != nil {
		if c.options.LogErrors {
			log.Printf("MDP client: failed to dial broker %s: %v", c.brokerEndpoint, err)
		}
		return fmt.Errorf("mdp: failed to connect to broker: %w", err)
	}
	
	if c.options.LogErrors {
		log.Printf("MDP client: socket dial successful, setting socket reference")
	}
	
	c.socket = socket
	c.running = true
	
	if c.options.LogErrors {
		log.Printf("MDP client: connected to %s, socket ready for requests", c.brokerEndpoint)
	}
	
	return nil
}

// Disconnect disconnects the client from the broker
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.running {
		return fmt.Errorf("mdp: client not connected")
	}
	
	c.running = false
	c.cancel()
	
	if c.socket != nil {
		err := c.socket.Close()
		if err != nil {
			return fmt.Errorf("mdp: failed to close client socket: %w", err)
		}
	}
	
	if c.options.LogErrors {
		log.Printf("MDP client disconnected")
	}
	
	return nil
}

// Request sends a request to a service and waits for a reply
func (c *Client) Request(service ServiceName, request []byte) ([]byte, error) {
	c.mu.RLock()
	running := c.running
	c.mu.RUnlock()
	
	if !running {
		if c.options.LogErrors {
			log.Printf("MDP client: request failed - client not connected")
		}
		return nil, fmt.Errorf("mdp: client not connected")
	}
	
	if err := service.Validate(); err != nil {
		if c.options.LogErrors {
			log.Printf("MDP client: request failed - invalid service name %s: %v", service, err)
		}
		return nil, fmt.Errorf("mdp: %w", err)
	}
	
	if c.options.LogErrors {
		log.Printf("MDP client: starting request to service %s (len=%d, retries=%d)", service, len(request), c.options.Retries)
	}
	
	var lastErr error
	
	for attempt := 0; attempt <= c.options.Retries; attempt++ {
		if attempt > 0 && c.options.LogErrors {
			log.Printf("MDP client: retrying request to %s (attempt %d/%d)", service, attempt+1, c.options.Retries+1)
		}
		
		reply, err := c.doRequest(service, request)
		if err == nil {
			c.mu.Lock()
			c.totalRequests++
			c.totalReplies++
			c.mu.Unlock()
			if c.options.LogErrors {
				log.Printf("MDP client: ✓ request to %s successful (reply len=%d)", service, len(reply))
			}
			return reply, nil
		}
		
		lastErr = err
		c.mu.Lock()
		c.totalErrors++
		c.mu.Unlock()
		
		if c.options.LogErrors {
			log.Printf("MDP client: request to %s failed: %v", service, err)
		}
		
		// Don't retry on the last attempt
		if attempt < c.options.Retries {
			// Reconnect for next attempt
			c.reconnect()
		}
	}
	
	if c.options.LogErrors {
		log.Printf("MDP client: ✗ request to %s failed after %d attempts", service, c.options.Retries+1)
	}
	return nil, fmt.Errorf("mdp: request failed after %d attempts: %w", c.options.Retries+1, lastErr)
}

// doRequest performs a single request attempt
func (c *Client) doRequest(service ServiceName, request []byte) ([]byte, error) {
	// Create and send request message
	msg := NewClientRequest(service, request)
	frames := msg.FormatClientRequest()
	
	if c.options.LogErrors {
		log.Printf("MDP client: formatting request message with %d frames:", len(frames))
		for i, frame := range frames {
			if len(frame) > 0 {
				log.Printf("  frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
	}
	
	zmqMsg := zmq4.NewMsgFrom(frames...)
	
	if c.options.LogErrors {
		log.Printf("MDP client: about to call socket.Send() for request to %s", service)
	}
	
	err := c.socket.Send(zmqMsg)
	if err != nil {
		if c.options.LogErrors {
			log.Printf("MDP client: socket.Send() failed: %v", err)
		}
		return nil, fmt.Errorf("mdp: failed to send request: %w", err)
	}
	
	if c.options.LogErrors {
		log.Printf("MDP client: ✓ socket.Send() successful, request sent to broker")
	}
	
	// Wait for reply with timeout
	ctx, cancel := context.WithTimeout(c.ctx, c.options.Timeout)
	defer cancel()
	
	done := make(chan zmq4.Msg, 1)
	errCh := make(chan error, 1)
	
	if c.options.LogErrors {
		log.Printf("MDP client: waiting for reply (timeout=%v)", c.options.Timeout)
		log.Printf("MDP client: starting socket.Recv() goroutine")
	}
	
	go func() {
		if c.options.LogErrors {
			log.Printf("MDP client: socket.Recv() goroutine started, calling socket.Recv()")
		}
		msg, err := c.socket.Recv()
		if c.options.LogErrors {
			if err != nil {
				log.Printf("MDP client: socket.Recv() returned error: %v", err)
			} else {
				log.Printf("MDP client: socket.Recv() returned %d frames successfully", len(msg.Frames))
				for i, frame := range msg.Frames {
					if len(frame) > 0 {
						log.Printf("  client_frame[%d]: %q (len=%d)", i, string(frame), len(frame))
					} else {
						log.Printf("  client_frame[%d]: <empty> (len=%d)", i, len(frame))
					}
				}
			}
		}
		if err != nil {
			errCh <- err
		} else {
			done <- msg
		}
	}()
	
	select {
	case msg := <-done:
		if c.options.LogErrors {
			log.Printf("MDP client: received reply with %d frames", len(msg.Frames))
		}
		return c.processReply(msg, service)
		
	case err := <-errCh:
		if c.options.LogErrors {
			log.Printf("MDP client: socket.Recv() failed: %v", err)
		}
		return nil, fmt.Errorf("mdp: failed to receive reply: %w", err)
		
	case <-ctx.Done():
		if c.options.LogErrors {
			log.Printf("MDP client: request timed out after %v", c.options.Timeout)
		}
		return nil, fmt.Errorf("mdp: request timeout after %v", c.options.Timeout)
	}
}

// processReply processes a reply message
func (c *Client) processReply(msg zmq4.Msg, expectedService ServiceName) ([]byte, error) {
	if c.options.LogErrors {
		log.Printf("MDP client: processReply called with %d frames:", len(msg.Frames))
		for i, frame := range msg.Frames {
			if len(frame) > 0 {
				log.Printf("  reply_frame[%d]: %q (len=%d)", i, string(frame), len(frame))
			} else {
				log.Printf("  reply_frame[%d]: <empty> (len=%d)", i, len(frame))
			}
		}
		log.Printf("MDP client: about to call ParseClientMessage()")
	}
	
	reply, err := ParseClientMessage(msg.Frames)
	if err != nil {
		if c.options.LogErrors {
			log.Printf("MDP client: ParseClientMessage failed: %v", err)
		}
		return nil, fmt.Errorf("mdp: invalid reply message: %w", err)
	}
	
	if c.options.LogErrors {
		log.Printf("MDP client: ParseClientMessage successful, service=%s, body_len=%d", reply.Service, len(reply.Body))
		log.Printf("MDP client: expected service=%s", expectedService)
	}
	
	if reply.Service != expectedService {
		if c.options.LogErrors {
			log.Printf("MDP client: service mismatch - got %s, expected %s", reply.Service, expectedService)
		}
		return nil, fmt.Errorf("mdp: service mismatch in reply: got %s, expected %s", reply.Service, expectedService)
	}
	
	if c.options.LogErrors {
		log.Printf("MDP client: processReply successful, returning body: %q", string(reply.Body))
	}
	
	return reply.Body, nil
}

// reconnect reconnects the client socket
func (c *Client) reconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.socket != nil {
		c.socket.Close()
	}
	
	// Create new socket with security if configured
	var socket zmq4.Socket
	if c.options.Security != nil {
		socket = zmq4.NewReq(c.ctx, zmq4.WithSecurity(c.options.Security))
	} else {
		socket = zmq4.NewReq(c.ctx)
	}
	err := socket.Dial(c.brokerEndpoint)
	if err != nil {
		if c.options.LogErrors {
			log.Printf("MDP client: failed to reconnect: %v", err)
		}
		return
	}
	
	c.socket = socket
	
	if c.options.LogErrors {
		log.Printf("MDP client: reconnected to %s", c.brokerEndpoint)
	}
}

// GetStats returns client statistics
func (c *Client) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return map[string]interface{}{
		"total_requests": c.totalRequests,
		"total_replies":  c.totalReplies,
		"total_errors":   c.totalErrors,
		"connected":      c.running,
	}
}

// AsyncClient implements an asynchronous MDP client using DEALER socket
type AsyncClient struct {
	// Configuration
	brokerEndpoint string
	options        *ClientOptions
	
	// Networking
	socket   zmq4.Socket
	ctx      context.Context
	cancel   context.CancelFunc
	
	// State
	mu       sync.RWMutex
	running  bool
	stopCh   chan struct{}
	
	// Request tracking
	pending  map[string]*pendingRequest // Key is request ID
	requestID uint64
	
	// Statistics
	totalRequests uint64
	totalReplies  uint64
	totalErrors   uint64
}

// pendingRequest represents a pending async request
type pendingRequest struct {
	Service   ServiceName
	Body      []byte
	Timestamp time.Time
	ReplyCh   chan *AsyncReply
}

// AsyncReply represents an asynchronous reply
type AsyncReply struct {
	Service ServiceName
	Body    []byte
	Error   error
}

// NewAsyncClient creates a new asynchronous MDP client
func NewAsyncClient(brokerEndpoint string, options *ClientOptions) *AsyncClient {
	if options == nil {
		options = DefaultClientOptions()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &AsyncClient{
		brokerEndpoint: brokerEndpoint,
		options:        options,
		ctx:            ctx,
		cancel:         cancel,
		stopCh:         make(chan struct{}),
		pending:        make(map[string]*pendingRequest),
	}
}

// Connect connects the async client to the broker
func (c *AsyncClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.running {
		return fmt.Errorf("mdp: async client already connected")
	}
	
	// Create DEALER socket for asynchronous client with security if configured
	var socket zmq4.Socket
	if c.options.Security != nil {
		socket = zmq4.NewDealer(c.ctx, zmq4.WithSecurity(c.options.Security))
	} else {
		socket = zmq4.NewDealer(c.ctx)
	}
	
	err := socket.Dial(c.brokerEndpoint)
	if err != nil {
		return fmt.Errorf("mdp: failed to connect to broker: %w", err)
	}
	
	c.socket = socket
	c.running = true
	
	// Start message processing loop
	go c.messageLoop()
	go c.cleanupLoop()
	
	if c.options.LogErrors {
		log.Printf("MDP async client connected to %s", c.brokerEndpoint)
	}
	
	return nil
}

// Disconnect disconnects the async client from the broker
func (c *AsyncClient) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.running {
		return fmt.Errorf("mdp: async client not connected")
	}
	
	c.running = false
	c.cancel()
	close(c.stopCh)
	
	if c.socket != nil {
		err := c.socket.Close()
		if err != nil {
			return fmt.Errorf("mdp: failed to close async client socket: %w", err)
		}
	}
	
	// Cancel all pending requests
	for _, pending := range c.pending {
		pending.ReplyCh <- &AsyncReply{
			Error: fmt.Errorf("mdp: client disconnected"),
		}
		close(pending.ReplyCh)
	}
	c.pending = make(map[string]*pendingRequest)
	
	if c.options.LogErrors {
		log.Printf("MDP async client disconnected")
	}
	
	return nil
}

// RequestAsync sends an asynchronous request to a service
func (c *AsyncClient) RequestAsync(service ServiceName, request []byte) (<-chan *AsyncReply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.running {
		return nil, fmt.Errorf("mdp: async client not connected")
	}
	
	if err := service.Validate(); err != nil {
		return nil, fmt.Errorf("mdp: %w", err)
	}
	
	// Generate unique request ID
	c.requestID++
	reqID := fmt.Sprintf("%d", c.requestID)
	
	// Create pending request
	replyCh := make(chan *AsyncReply, 1)
	pending := &pendingRequest{
		Service:   service,
		Body:      request,
		Timestamp: time.Now(),
		ReplyCh:   replyCh,
	}
	
	c.pending[reqID] = pending
	
	// Create and send request message
	msg := NewClientRequest(service, request)
	frames := msg.FormatClientRequest()
	
	// Prepend request ID for tracking
	allFrames := make([][]byte, 0, len(frames)+1)
	allFrames = append(allFrames, []byte(reqID))
	allFrames = append(allFrames, frames...)
	
	zmqMsg := zmq4.NewMsgFrom(allFrames...)
	
	err := c.socket.Send(zmqMsg)
	if err != nil {
		delete(c.pending, reqID)
		close(replyCh)
		return nil, fmt.Errorf("mdp: failed to send async request: %w", err)
	}
	
	c.totalRequests++
	
	return replyCh, nil
}

// messageLoop processes incoming messages
func (c *AsyncClient) messageLoop() {
	for {
		select {
		case <-c.stopCh:
			return
		default:
			msg, err := c.socket.Recv()
			if err != nil {
				if c.running && c.options.LogErrors {
					log.Printf("MDP async client recv error: %v", err)
				}
				continue
			}
			
			c.processReply(msg)
		}
	}
}

// processReply processes a reply message for async client
func (c *AsyncClient) processReply(msg zmq4.Msg) {
	frames := msg.Frames
	if len(frames) < 1 {
		if c.options.LogErrors {
			log.Printf("MDP async client: empty reply message")
		}
		return
	}
	
	// First frame should be request ID
	reqID := string(frames[0])
	
	c.mu.Lock()
	pending, exists := c.pending[reqID]
	if exists {
		delete(c.pending, reqID)
	}
	c.mu.Unlock()
	
	if !exists {
		if c.options.LogErrors {
			log.Printf("MDP async client: unknown request ID: %s", reqID)
		}
		return
	}
	
	// Parse reply
	reply, err := ParseClientMessage(frames[1:])
	if err != nil {
		pending.ReplyCh <- &AsyncReply{
			Error: fmt.Errorf("mdp: invalid reply message: %w", err),
		}
		close(pending.ReplyCh)
		c.mu.Lock()
		c.totalErrors++
		c.mu.Unlock()
		return
	}
	
	if reply.Service != pending.Service {
		pending.ReplyCh <- &AsyncReply{
			Error: fmt.Errorf("mdp: service mismatch in reply: got %s, expected %s", reply.Service, pending.Service),
		}
		close(pending.ReplyCh)
		c.mu.Lock()
		c.totalErrors++
		c.mu.Unlock()
		return
	}
	
	// Send successful reply
	pending.ReplyCh <- &AsyncReply{
		Service: reply.Service,
		Body:    reply.Body,
		Error:   nil,
	}
	close(pending.ReplyCh)
	
	c.mu.Lock()
	c.totalReplies++
	c.mu.Unlock()
}

// cleanupLoop removes expired pending requests
func (c *AsyncClient) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopCh:
			return
		}
	}
}

// cleanup removes expired pending requests
func (c *AsyncClient) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	for reqID, pending := range c.pending {
		if now.Sub(pending.Timestamp) > c.options.Timeout {
			pending.ReplyCh <- &AsyncReply{
				Error: fmt.Errorf("mdp: request timeout after %v", c.options.Timeout),
			}
			close(pending.ReplyCh)
			delete(c.pending, reqID)
			c.totalErrors++
		}
	}
}

// GetStats returns async client statistics
func (c *AsyncClient) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return map[string]interface{}{
		"total_requests":  c.totalRequests,
		"total_replies":   c.totalReplies,
		"total_errors":    c.totalErrors,
		"pending_requests": len(c.pending),
		"connected":       c.running,
	}
}