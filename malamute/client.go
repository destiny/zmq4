// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package malamute

import (
	"context"
	"fmt"
	"sync"
	"time"
	"crypto/rand"
	"encoding/hex"

	"github.com/destiny/zmq4/v25"
)

// MalamuteClient represents a Malamute client connection
type MalamuteClient struct {
	// Configuration
	config       *ClientConfig     // Client configuration
	clientID     string            // Client identifier
	clientType   int               // Client type
	
	// Network components
	dealerSocket zmq4.Socket       // DEALER socket for broker connection
	
	// Channel-based communication
	outgoing     chan *Message     // Outgoing messages to broker
	incoming     chan *Message     // Incoming messages from broker
	commands     chan clientCmd    // Client control commands
	responses    chan *MessageResponse // Response handling
	
	// State management
	state        int               // Client state
	ctx          context.Context   // Context for cancellation
	cancel       context.CancelFunc // Cancel function
	wg           sync.WaitGroup    // Wait group for goroutines
	mutex        sync.RWMutex      // Protects client state
	sequenceNum  uint32            // Message sequence counter
	
	// Credit tracking
	credit       int32             // Available credit
	creditChan   chan int32        // Credit updates
	
	// Response tracking
	pendingResponses map[uint32]chan *MessageResponse // Pending response channels
}

// MessageResponse represents a response to a message
type MessageResponse struct {
	Message   *Message  // Response message
	Error     error     // Error if any
	Timestamp time.Time // Response timestamp
}

// clientCmd represents internal client commands
type clientCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// StreamPublisher provides stream publishing interface
type StreamPublisher struct {
	client *MalamuteClient
}

// StreamSubscriber provides stream subscription interface
type StreamSubscriber struct {
	client     *MalamuteClient
	stream     string
	pattern    string
	messages   chan *StreamSendMessage
	cancel     context.CancelFunc
}

// MailboxClient provides mailbox messaging interface
type MailboxClient struct {
	client  *MalamuteClient
	address string
}

// ServiceWorker provides service worker interface
type ServiceWorker struct {
	client   *MalamuteClient
	service  string
	pattern  string
	requests chan *ServiceDeliverMessage
	cancel   context.CancelFunc
}

// ServiceClient provides service client interface
type ServiceClient struct {
	client *MalamuteClient
}

// NewMalamuteClient creates a new Malamute client
func NewMalamuteClient(config *ClientConfig) (*MalamuteClient, error) {
	if config == nil {
		config = &ClientConfig{
			ClientID:          generateClientID(),
			BrokerEndpoint:    DefaultEndpoint,
			HeartbeatInterval: DefaultHeartbeatInterval,
			Timeout:           DefaultClientTimeout,
			CreditWindow:      DefaultCreditWindow,
			MaxRetries:        3,
			RetryInterval:     DefaultRetryInterval,
		}
	}
	
	if config.ClientID == "" {
		config.ClientID = generateClientID()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	client := &MalamuteClient{
		config:           config,
		clientID:         config.ClientID,
		clientType:       ClientTypeGeneral,
		outgoing:         make(chan *Message, 1000),
		incoming:         make(chan *Message, 1000),
		commands:         make(chan clientCmd, 100),
		responses:        make(chan *MessageResponse, 1000),
		state:            0, // Disconnected
		ctx:              ctx,
		cancel:           cancel,
		sequenceNum:      0,
		credit:           0,
		creditChan:       make(chan int32, 100),
		pendingResponses: make(map[uint32]chan *MessageResponse),
	}
	
	return client, nil
}

// Connect connects to the Malamute broker
func (c *MalamuteClient) Connect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	if c.state != 0 {
		return fmt.Errorf("client already connected")
	}
	
	c.state = 1 // Connecting
	
	// Create and connect DEALER socket
	c.dealerSocket = zmq4.NewDealer(c.ctx)
	if err := c.dealerSocket.Dial(c.config.BrokerEndpoint); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}
	
	// Start internal loops
	c.wg.Add(1)
	go c.socketLoop()
	
	c.wg.Add(1)
	go c.outgoingLoop()
	
	c.wg.Add(1)
	go c.incomingLoop()
	
	c.wg.Add(1)
	go c.commandLoop()
	
	c.wg.Add(1)
	go c.heartbeatLoop()
	
	// Send connection open message
	openMsg := &ConnectionOpenMessage{
		ClientID:   c.clientID,
		ClientType: c.clientType,
		Attributes: make(map[string]string),
	}
	
	body, _ := openMsg.Marshal()
	message := CreateMessage(MessageTypeConnectionOpen, c.clientID, c.nextSequence())
	message.Body = body
	
	// Send and wait for response
	respChan := make(chan *MessageResponse, 1)
	c.mutex.Lock()
	c.pendingResponses[message.Sequence] = respChan
	c.mutex.Unlock()
	
	select {
	case c.outgoing <- message:
		// Message sent
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-time.After(c.config.Timeout):
		return fmt.Errorf("connection timeout")
	}
	
	// Wait for response
	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return resp.Error
		}
		c.state = 2 // Connected
		return nil
	case <-time.After(c.config.Timeout):
		return fmt.Errorf("connection timeout")
	}
}

// Disconnect disconnects from the broker
func (c *MalamuteClient) Disconnect() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	if c.state == 0 {
		return
	}
	
	c.state = 3 // Disconnecting
	
	// Send close message
	message := CreateMessage(MessageTypeConnectionClose, c.clientID, c.nextSequence())
	select {
	case c.outgoing <- message:
	default:
	}
	
	// Cancel context and wait
	c.cancel()
	c.wg.Wait()
	
	// Close socket
	if c.dealerSocket != nil {
		c.dealerSocket.Close()
	}
	
	// Close channels
	close(c.outgoing)
	close(c.incoming)
	close(c.commands)
	close(c.responses)
	close(c.creditChan)
	
	c.state = 0 // Disconnected
}

// Publisher creates a stream publisher
func (c *MalamuteClient) Publisher() *StreamPublisher {
	c.clientType = ClientTypePublisher
	return &StreamPublisher{client: c}
}

// Subscriber creates a stream subscriber
func (c *MalamuteClient) Subscriber() *StreamSubscriber {
	c.clientType = ClientTypeSubscriber
	return &StreamSubscriber{client: c}
}

// Mailbox creates a mailbox client
func (c *MalamuteClient) Mailbox(address string) *MailboxClient {
	return &MailboxClient{
		client:  c,
		address: address,
	}
}

// Worker creates a service worker
func (c *MalamuteClient) Worker(service, pattern string) *ServiceWorker {
	c.clientType = ClientTypeWorker
	ctx, cancel := context.WithCancel(c.ctx)
	
	return &ServiceWorker{
		client:   c,
		service:  service,
		pattern:  pattern,
		requests: make(chan *ServiceDeliverMessage, 1000),
		cancel:   cancel,
	}
}

// ServiceClient creates a service client
func (c *MalamuteClient) ServiceClient() *ServiceClient {
	c.clientType = ClientTypeRequester
	return &ServiceClient{client: c}
}

// GetCredit returns current credit
func (c *MalamuteClient) GetCredit() int32 {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.credit
}

// RequestCredit requests credit from broker
func (c *MalamuteClient) RequestCredit(amount int32) error {
	creditMsg := &CreditRequestMessage{
		Credit: amount,
	}
	
	body, _ := creditMsg.Marshal()
	message := CreateMessage(MessageTypeCreditRequest, c.clientID, c.nextSequence())
	message.Body = body
	
	select {
	case c.outgoing <- message:
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
		return fmt.Errorf("outgoing queue full")
	}
}

// socketLoop handles socket I/O
func (c *MalamuteClient) socketLoop() {
	defer c.wg.Done()
	
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			// Receive message from broker
			msg, err := c.dealerSocket.Recv()
			if err != nil {
				continue
			}
			
			// Parse message
			frames := msg.Frames()
			if len(frames) < 1 {
				continue
			}
			
			var message Message
			if err := message.Unmarshal(frames[0]); err != nil {
				continue
			}
			
			// Queue for processing
			select {
			case c.incoming <- &message:
			default:
				// Incoming queue full
			}
		}
	}
}

// outgoingLoop sends messages to broker
func (c *MalamuteClient) outgoingLoop() {
	defer c.wg.Done()
	
	for {
		select {
		case message := <-c.outgoing:
			c.sendMessage(message)
		case <-c.ctx.Done():
			return
		}
	}
}

// incomingLoop processes messages from broker
func (c *MalamuteClient) incomingLoop() {
	defer c.wg.Done()
	
	for {
		select {
		case message := <-c.incoming:
			c.processIncoming(message)
		case <-c.ctx.Done():
			return
		}
	}
}

// commandLoop processes client commands
func (c *MalamuteClient) commandLoop() {
	defer c.wg.Done()
	
	for {
		select {
		case cmd := <-c.commands:
			c.handleCommand(cmd)
		case <-c.ctx.Done():
			return
		}
	}
}

// heartbeatLoop sends periodic heartbeats
func (c *MalamuteClient) heartbeatLoop() {
	defer c.wg.Done()
	
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			c.sendHeartbeat()
		case <-c.ctx.Done():
			return
		}
	}
}

// sendMessage sends a message to the broker
func (c *MalamuteClient) sendMessage(message *Message) {
	data, err := message.Marshal()
	if err != nil {
		return
	}
	
	msg := zmq4.NewMsgFromBytes(data)
	c.dealerSocket.SendMulti(msg)
}

// processIncoming processes an incoming message
func (c *MalamuteClient) processIncoming(message *Message) {
	switch message.Type {
	case MessageTypeConnectionPong:
		// Handle heartbeat response
		
	case MessageTypeCreditConfirm:
		var creditMsg CreditConfirmMessage
		if err := creditMsg.Unmarshal(message.Body); err == nil {
			c.mutex.Lock()
			c.credit += creditMsg.Credit
			c.mutex.Unlock()
			
			select {
			case c.creditChan <- creditMsg.Credit:
			default:
			}
		}
		
	case MessageTypeStreamSend:
		// Handle stream message delivery
		
	case MessageTypeMailboxDeliver:
		// Handle mailbox message delivery
		
	case MessageTypeServiceDeliver:
		// Handle service request delivery
		
	case MessageTypeError:
		// Handle error response
		
	default:
		// Check for pending response
		c.mutex.Lock()
		if respChan, exists := c.pendingResponses[message.Sequence]; exists {
			delete(c.pendingResponses, message.Sequence)
			c.mutex.Unlock()
			
			response := &MessageResponse{
				Message:   message,
				Timestamp: time.Now(),
			}
			
			select {
			case respChan <- response:
			default:
			}
		} else {
			c.mutex.Unlock()
		}
	}
}

// handleCommand processes a client command
func (c *MalamuteClient) handleCommand(cmd clientCmd) {
	// Implementation for internal commands
}

// sendHeartbeat sends a heartbeat to the broker
func (c *MalamuteClient) sendHeartbeat() {
	if c.state != 2 { // Not connected
		return
	}
	
	pingMsg := &ConnectionPingMessage{
		Timestamp: time.Now().UnixNano(),
	}
	
	body, _ := pingMsg.Marshal()
	message := CreateMessage(MessageTypeConnectionPing, c.clientID, c.nextSequence())
	message.Body = body
	
	select {
	case c.outgoing <- message:
	default:
	}
}

// nextSequence returns the next sequence number
func (c *MalamuteClient) nextSequence() uint32 {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.sequenceNum++
	return c.sequenceNum
}

// generateClientID generates a unique client ID
func generateClientID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return "client-" + hex.EncodeToString(bytes)
}

// StreamPublisher implementation

// Publish publishes a message to a stream
func (sp *StreamPublisher) Publish(stream, subject string, body []byte) error {
	return sp.PublishWithAttributes(stream, subject, body, nil)
}

// PublishWithAttributes publishes a message with attributes
func (sp *StreamPublisher) PublishWithAttributes(stream, subject string, body []byte, attributes map[string]string) error {
	writeMsg := &StreamWriteMessage{
		Stream:     stream,
		Subject:    subject,
		Attributes: attributes,
		Body:       body,
	}
	
	msgBody, _ := writeMsg.Marshal()
	message := CreateMessage(MessageTypeStreamWrite, sp.client.clientID, sp.client.nextSequence())
	message.Body = msgBody
	
	select {
	case sp.client.outgoing <- message:
		return nil
	case <-sp.client.ctx.Done():
		return sp.client.ctx.Err()
	default:
		return fmt.Errorf("outgoing queue full")
	}
}

// StreamSubscriber implementation

// Subscribe subscribes to a stream
func (ss *StreamSubscriber) Subscribe(stream, pattern string) error {
	ss.stream = stream
	ss.pattern = pattern
	ss.messages = make(chan *StreamSendMessage, 1000)
	
	readMsg := &StreamReadMessage{
		Stream:     stream,
		Pattern:    pattern,
		Attributes: make(map[string]string),
	}
	
	body, _ := readMsg.Marshal()
	message := CreateMessage(MessageTypeStreamRead, ss.client.clientID, ss.client.nextSequence())
	message.Body = body
	
	select {
	case ss.client.outgoing <- message:
		return nil
	case <-ss.client.ctx.Done():
		return ss.client.ctx.Err()
	default:
		return fmt.Errorf("outgoing queue full")
	}
}

// Messages returns the message channel
func (ss *StreamSubscriber) Messages() <-chan *StreamSendMessage {
	return ss.messages
}

// MailboxClient implementation

// Send sends a message to the mailbox
func (mc *MailboxClient) Send(subject string, body []byte) error {
	return mc.SendWithTracker(subject, body, "", 0)
}

// SendWithTracker sends a message with tracker and timeout
func (mc *MailboxClient) SendWithTracker(subject string, body []byte, tracker string, timeout time.Duration) error {
	sendMsg := &MailboxSendMessage{
		Address:    mc.address,
		Subject:    subject,
		Tracker:    tracker,
		Timeout:    int64(timeout / time.Millisecond),
		Attributes: make(map[string]string),
		Body:       body,
	}
	
	msgBody, _ := sendMsg.Marshal()
	message := CreateMessage(MessageTypeMailboxSend, mc.client.clientID, mc.client.nextSequence())
	message.Body = msgBody
	
	select {
	case mc.client.outgoing <- message:
		return nil
	case <-mc.client.ctx.Done():
		return mc.client.ctx.Err()
	default:
		return fmt.Errorf("outgoing queue full")
	}
}

// ServiceWorker implementation

// Start starts the service worker
func (sw *ServiceWorker) Start() error {
	offerMsg := &ServiceOfferMessage{
		Service:    sw.service,
		Pattern:    sw.pattern,
		Attributes: make(map[string]string),
	}
	
	body, _ := offerMsg.Marshal()
	message := CreateMessage(MessageTypeServiceOffer, sw.client.clientID, sw.client.nextSequence())
	message.Body = body
	
	select {
	case sw.client.outgoing <- message:
		return nil
	case <-sw.client.ctx.Done():
		return sw.client.ctx.Err()
	default:
		return fmt.Errorf("outgoing queue full")
	}
}

// Requests returns the request channel
func (sw *ServiceWorker) Requests() <-chan *ServiceDeliverMessage {
	return sw.requests
}

// Reply sends a reply to a service request
func (sw *ServiceWorker) Reply(tracker string, status int, body []byte) error {
	replyMsg := &ServiceReplyMessage{
		Tracker:    tracker,
		Status:     status,
		Attributes: make(map[string]string),
		Body:       body,
	}
	
	msgBody, _ := replyMsg.Marshal()
	message := CreateMessage(MessageTypeServiceReply, sw.client.clientID, sw.client.nextSequence())
	message.Body = msgBody
	
	select {
	case sw.client.outgoing <- message:
		return nil
	case <-sw.client.ctx.Done():
		return sw.client.ctx.Err()
	default:
		return fmt.Errorf("outgoing queue full")
	}
}

// ServiceClient implementation

// Request sends a service request
func (sc *ServiceClient) Request(service, method, tracker string, body []byte, timeout time.Duration) error {
	requestMsg := &ServiceRequestMessage{
		Service:    service,
		Method:     method,
		Tracker:    tracker,
		Timeout:    int64(timeout / time.Millisecond),
		Attributes: make(map[string]string),
		Body:       body,
	}
	
	msgBody, _ := requestMsg.Marshal()
	message := CreateMessage(MessageTypeServiceRequest, sc.client.clientID, sc.client.nextSequence())
	message.Body = msgBody
	
	select {
	case sc.client.outgoing <- message:
		return nil
	case <-sc.client.ctx.Done():
		return sc.client.ctx.Err()
	default:
		return fmt.Errorf("outgoing queue full")
	}
}