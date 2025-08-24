// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package malamute

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/destiny/zmq4/v25"
)

// Broker represents a Malamute enterprise messaging broker
type Broker struct {
	// Configuration
	config *BrokerConfig // Broker configuration
	name   string        // Broker name
	
	// Network components
	routerSocket zmq4.Socket // ROUTER socket for client connections
	
	// Core subsystems
	creditController *CreditController     // Credit-based flow control
	streamManager    *StreamManager       // Stream management
	mailboxManager   *MailboxManager      // Mailbox management
	serviceManager   *ServiceManager      // Service management
	
	// Client management
	clients          map[string]*Client    // Connected clients
	
	// Channel-based communication
	messages         chan *ClientMessage   // Incoming client messages
	responses        chan *ClientResponse  // Outgoing responses
	commands         chan brokerCmd        // Broker control commands
	heartbeats       chan *HeartbeatEvent  // Heartbeat events
	
	// State management
	state            int             // Broker state
	ctx              context.Context // Context for cancellation
	cancel           context.CancelFunc // Cancel function
	wg               sync.WaitGroup  // Wait group for goroutines
	mutex            sync.RWMutex    // Protects broker state
	statistics       *Statistics    // Broker statistics
	sequenceCounter  uint32         // Message sequence counter
}

// ClientMessage represents a message from a client
type ClientMessage struct {
	ClientID  string    // Client identifier
	Message   *Message  // The message
	Timestamp time.Time // When message was received
}

// ClientResponse represents a response to send to a client
type ClientResponse struct {
	ClientID  string    // Target client
	Message   *Message  // Response message
	Timestamp time.Time // When response was created
}

// HeartbeatEvent represents a heartbeat event
type HeartbeatEvent struct {
	ClientID  string    // Client identifier
	Timestamp time.Time // Heartbeat timestamp
	Type      string    // Event type (ping, pong, timeout)
}

// brokerCmd represents internal broker commands
type brokerCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// NewBroker creates a new Malamute broker
func NewBroker(config *BrokerConfig) (*Broker, error) {
	if config == nil {
		config = &BrokerConfig{
			Name:               "malamute",
			Endpoint:           DefaultEndpoint,
			CreditWindow:       DefaultCreditWindow,
			HeartbeatInterval:  DefaultHeartbeatInterval,
			ClientTimeout:      DefaultClientTimeout,
			ServiceTimeout:     DefaultServiceTimeout,
			MaxConnections:     1000,
			MaxStreams:         1000,
			MaxMailboxes:       10000,
			MaxServices:        1000,
			StreamRetention:    24 * time.Hour,
			MailboxRetention:   7 * 24 * time.Hour,
		}
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	broker := &Broker{
		config:           config,
		name:             config.Name,
		clients:          make(map[string]*Client),
		messages:         make(chan *ClientMessage, 10000),
		responses:        make(chan *ClientResponse, 10000),
		commands:         make(chan brokerCmd, 1000),
		heartbeats:       make(chan *HeartbeatEvent, 1000),
		state:            0, // Stopped
		ctx:              ctx,
		cancel:           cancel,
		statistics:       &Statistics{StartTime: time.Now()},
		sequenceCounter:  0,
	}
	
	// Create subsystems
	broker.creditController = NewCreditController(config.CreditWindow)
	broker.streamManager = NewStreamManager(config.MaxStreams, config.StreamRetention)
	broker.mailboxManager = NewMailboxManager(config.MaxMailboxes, config.MailboxRetention)
	broker.serviceManager = NewServiceManager(config.MaxServices, config.ServiceTimeout)
	
	return broker, nil
}

// Start begins broker operation
func (b *Broker) Start() error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	
	if b.state != 0 {
		return fmt.Errorf("broker already started")
	}
	
	b.state = 1 // Starting
	
	// Create and bind ROUTER socket
	b.routerSocket = zmq4.NewRouter(b.ctx)
	if err := b.routerSocket.Listen(b.config.Endpoint); err != nil {
		return fmt.Errorf("failed to bind ROUTER socket: %w", err)
	}
	
	// Start subsystems
	b.creditController.Start()
	b.streamManager.Start()
	b.mailboxManager.Start()
	b.serviceManager.Start()
	
	// Start internal loops
	b.wg.Add(1)
	go b.socketLoop()
	
	b.wg.Add(1)
	go b.messageLoop()
	
	b.wg.Add(1)
	go b.responseLoop()
	
	b.wg.Add(1)
	go b.commandLoop()
	
	b.wg.Add(1)
	go b.heartbeatLoop()
	
	b.wg.Add(1)
	go b.cleanupLoop()
	
	b.state = 2 // Running
	
	fmt.Printf("Malamute broker '%s' started on %s\n", b.name, b.config.Endpoint)
	
	return nil
}

// Stop stops the broker
func (b *Broker) Stop() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	
	if b.state == 0 {
		return
	}
	
	b.state = 3 // Stopping
	
	fmt.Printf("Stopping Malamute broker '%s'...\n", b.name)
	
	// Cancel context and wait for goroutines
	b.cancel()
	b.wg.Wait()
	
	// Stop subsystems
	b.creditController.Stop()
	b.streamManager.Stop()
	b.mailboxManager.Stop()
	b.serviceManager.Stop()
	
	// Close socket
	if b.routerSocket != nil {
		b.routerSocket.Close()
	}
	
	// Close channels
	close(b.messages)
	close(b.responses)
	close(b.commands)
	close(b.heartbeats)
	
	b.state = 0 // Stopped
	
	fmt.Printf("Malamute broker '%s' stopped\n", b.name)
}

// GetStatistics returns broker statistics
func (b *Broker) GetStatistics() *Statistics {
	reply := make(chan interface{}, 1)
	cmd := brokerCmd{
		action: "get_statistics",
		reply:  reply,
	}
	
	select {
	case b.commands <- cmd:
		result := <-reply
		return result.(*Statistics)
	case <-b.ctx.Done():
		return &Statistics{}
	}
}

// GetClients returns information about connected clients
func (b *Broker) GetClients() map[string]*Client {
	reply := make(chan interface{}, 1)
	cmd := brokerCmd{
		action: "get_clients",
		reply:  reply,
	}
	
	select {
	case b.commands <- cmd:
		result := <-reply
		return result.(map[string]*Client)
	case <-b.ctx.Done():
		return make(map[string]*Client)
	}
}

// socketLoop handles socket I/O
func (b *Broker) socketLoop() {
	defer b.wg.Done()
	
	for {
		select {
		case <-b.ctx.Done():
			return
		default:
			// Receive message from client
			msg, err := b.routerSocket.Recv()
			if err != nil {
				continue
			}
			
			// Extract client ID from first frame
			frames := msg.Frames()
			if len(frames) < 2 {
				continue
			}
			
			clientID := string(frames[0])
			messageData := frames[1]
			
			// Parse message
			var message Message
			if err := message.Unmarshal(messageData); err != nil {
				b.sendError(clientID, ErrorCodeInvalidMessage, "Invalid message format", 0)
				continue
			}
			
			// Queue message for processing
			clientMessage := &ClientMessage{
				ClientID:  clientID,
				Message:   &message,
				Timestamp: time.Now(),
			}
			
			select {
			case b.messages <- clientMessage:
				// Message queued successfully
				b.statistics.MessagesReceived++
				b.statistics.BytesReceived += uint64(len(messageData))
			default:
				// Message queue full
				b.sendError(clientID, ErrorCodeInternalError, "Message queue full", message.Sequence)
			}
		}
	}
}

// messageLoop processes incoming client messages
func (b *Broker) messageLoop() {
	defer b.wg.Done()
	
	for {
		select {
		case clientMessage := <-b.messages:
			b.processClientMessage(clientMessage)
		case <-b.ctx.Done():
			return
		}
	}
}

// responseLoop sends responses to clients
func (b *Broker) responseLoop() {
	defer b.wg.Done()
	
	for {
		select {
		case response := <-b.responses:
			b.sendResponse(response)
		case <-b.ctx.Done():
			return
		}
	}
}

// commandLoop processes broker commands
func (b *Broker) commandLoop() {
	defer b.wg.Done()
	
	for {
		select {
		case cmd := <-b.commands:
			b.handleCommand(cmd)
		case <-b.ctx.Done():
			return
		}
	}
}

// heartbeatLoop handles client heartbeats
func (b *Broker) heartbeatLoop() {
	defer b.wg.Done()
	
	ticker := time.NewTicker(b.config.HeartbeatInterval)
	defer ticker.Stop()
	
	for {
		select {
		case heartbeat := <-b.heartbeats:
			b.processHeartbeat(heartbeat)
		case <-ticker.C:
			b.checkClientTimeouts()
		case <-b.ctx.Done():
			return
		}
	}
}

// cleanupLoop periodically cleans up resources
func (b *Broker) cleanupLoop() {
	defer b.wg.Done()
	
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			b.performCleanup()
		case <-b.ctx.Done():
			return
		}
	}
}

// processClientMessage processes a message from a client
func (b *Broker) processClientMessage(clientMessage *ClientMessage) {
	clientID := clientMessage.ClientID
	message := clientMessage.Message
	
	// Update client last activity
	b.updateClientActivity(clientID)
	
	// Process based on message type
	switch message.Type {
	case MessageTypeConnectionOpen:
		b.handleConnectionOpen(clientID, message)
	case MessageTypeConnectionPing:
		b.handleConnectionPing(clientID, message)
	case MessageTypeConnectionClose:
		b.handleConnectionClose(clientID, message)
	case MessageTypeStreamWrite:
		b.handleStreamWrite(clientID, message)
	case MessageTypeStreamRead:
		b.handleStreamRead(clientID, message)
	case MessageTypeStreamCancel:
		b.handleStreamCancel(clientID, message)
	case MessageTypeMailboxSend:
		b.handleMailboxSend(clientID, message)
	case MessageTypeMailboxReceive:
		b.handleMailboxReceive(clientID, message)
	case MessageTypeServiceOffer:
		b.handleServiceOffer(clientID, message)
	case MessageTypeServiceWithdraw:
		b.handleServiceWithdraw(clientID, message)
	case MessageTypeServiceRequest:
		b.handleServiceRequest(clientID, message)
	case MessageTypeServiceReply:
		b.handleServiceReply(clientID, message)
	case MessageTypeCreditRequest:
		b.handleCreditRequest(clientID, message)
	default:
		b.sendError(clientID, ErrorCodeNotImplemented, "Message type not implemented", message.Sequence)
	}
}

// handleConnectionOpen handles a connection open request
func (b *Broker) handleConnectionOpen(clientID string, message *Message) {
	var openMsg ConnectionOpenMessage
	if err := openMsg.Unmarshal(message.Body); err != nil {
		b.sendError(clientID, ErrorCodeInvalidMessage, "Invalid connection open message", message.Sequence)
		return
	}
	
	// Check if we have too many connections
	b.mutex.RLock()
	clientCount := len(b.clients)
	b.mutex.RUnlock()
	
	if clientCount >= b.config.MaxConnections {
		b.sendError(clientID, ErrorCodeQuotaExceeded, "Maximum connections exceeded", message.Sequence)
		return
	}
	
	// Create client
	client := &Client{
		ID:           openMsg.ClientID,
		Type:         openMsg.ClientType,
		Address:      clientID, // Socket identity
		Credit:       0,
		Attributes:   openMsg.Attributes,
		ConnectedAt:  time.Now(),
		LastActivity: time.Now(),
	}
	
	// Register client
	b.mutex.Lock()
	b.clients[clientID] = client
	b.mutex.Unlock()
	
	b.statistics.TotalConnections++
	b.statistics.ActiveConnections++
	
	// Send confirmation (implementation would send success response)
	fmt.Printf("Client %s connected (type=%d)\n", openMsg.ClientID, openMsg.ClientType)
}

// handleConnectionPing handles a heartbeat ping
func (b *Broker) handleConnectionPing(clientID string, message *Message) {
	var pingMsg ConnectionPingMessage
	if err := pingMsg.Unmarshal(message.Body); err != nil {
		return
	}
	
	// Send pong response
	pongMsg := &ConnectionPongMessage{
		Timestamp: pingMsg.Timestamp,
	}
	
	body, _ := pongMsg.Marshal()
	response := CreateMessage(MessageTypeConnectionPong, clientID, b.nextSequence())
	response.Body = body
	
	b.queueResponse(clientID, response)
	
	// Record heartbeat
	heartbeat := &HeartbeatEvent{
		ClientID:  clientID,
		Timestamp: time.Now(),
		Type:      "ping",
	}
	
	select {
	case b.heartbeats <- heartbeat:
	default:
		// Heartbeat queue full
	}
}

// handleConnectionClose handles a connection close request
func (b *Broker) handleConnectionClose(clientID string, message *Message) {
	b.removeClient(clientID)
}

// handleStreamWrite handles a stream write request
func (b *Broker) handleStreamWrite(clientID string, message *Message) {
	// Check credit
	if !b.creditController.CheckCredit(clientID, 1) {
		b.sendError(clientID, ErrorCodeQuotaExceeded, "Insufficient credit", message.Sequence)
		return
	}
	
	var writeMsg StreamWriteMessage
	if err := writeMsg.Unmarshal(message.Body); err != nil {
		b.sendError(clientID, ErrorCodeInvalidMessage, "Invalid stream write message", message.Sequence)
		return
	}
	
	// Use credit
	b.creditController.UseCredit(clientID, 1)
	
	// Forward to stream manager
	err := b.streamManager.WriteMessage(writeMsg.Stream, writeMsg.Subject, clientID, writeMsg.Body, writeMsg.Attributes)
	if err != nil {
		b.sendError(clientID, ErrorCodeInternalError, err.Error(), message.Sequence)
		return
	}
	
	b.statistics.StreamMessages++
	b.statistics.StreamBytes += uint64(len(writeMsg.Body))
}

// handleStreamRead handles a stream read request
func (b *Broker) handleStreamRead(clientID string, message *Message) {
	var readMsg StreamReadMessage
	if err := readMsg.Unmarshal(message.Body); err != nil {
		b.sendError(clientID, ErrorCodeInvalidMessage, "Invalid stream read message", message.Sequence)
		return
	}
	
	// Forward to stream manager
	err := b.streamManager.Subscribe(clientID, readMsg.Stream, readMsg.Pattern, readMsg.Attributes)
	if err != nil {
		b.sendError(clientID, ErrorCodeInternalError, err.Error(), message.Sequence)
		return
	}
}

// handleStreamCancel handles a stream cancel request
func (b *Broker) handleStreamCancel(clientID string, message *Message) {
	// Implementation would handle stream cancellation
}

// handleMailboxSend handles a mailbox send request
func (b *Broker) handleMailboxSend(clientID string, message *Message) {
	var sendMsg MailboxSendMessage
	if err := sendMsg.Unmarshal(message.Body); err != nil {
		b.sendError(clientID, ErrorCodeInvalidMessage, "Invalid mailbox send message", message.Sequence)
		return
	}
	
	// Forward to mailbox manager
	err := b.mailboxManager.SendMessage(sendMsg.Address, sendMsg.Subject, clientID, sendMsg.Tracker, sendMsg.Body, sendMsg.Attributes, time.Duration(sendMsg.Timeout)*time.Millisecond)
	if err != nil {
		b.sendError(clientID, ErrorCodeInternalError, err.Error(), message.Sequence)
		return
	}
	
	b.statistics.MailboxMessages++
	b.statistics.MailboxBytes += uint64(len(sendMsg.Body))
}

// handleMailboxReceive handles a mailbox receive request
func (b *Broker) handleMailboxReceive(clientID string, message *Message) {
	// Implementation would handle mailbox receive
}

// handleServiceOffer handles a service offer
func (b *Broker) handleServiceOffer(clientID string, message *Message) {
	var offerMsg ServiceOfferMessage
	if err := offerMsg.Unmarshal(message.Body); err != nil {
		b.sendError(clientID, ErrorCodeInvalidMessage, "Invalid service offer message", message.Sequence)
		return
	}
	
	// Forward to service manager
	err := b.serviceManager.RegisterWorker(offerMsg.Service, clientID, offerMsg.Pattern, offerMsg.Attributes)
	if err != nil {
		b.sendError(clientID, ErrorCodeInternalError, err.Error(), message.Sequence)
		return
	}
	
	b.statistics.ServicesRegistered++
}

// handleServiceWithdraw handles a service withdraw
func (b *Broker) handleServiceWithdraw(clientID string, message *Message) {
	// Implementation would handle service withdrawal
}

// handleServiceRequest handles a service request
func (b *Broker) handleServiceRequest(clientID string, message *Message) {
	var requestMsg ServiceRequestMessage
	if err := requestMsg.Unmarshal(message.Body); err != nil {
		b.sendError(clientID, ErrorCodeInvalidMessage, "Invalid service request message", message.Sequence)
		return
	}
	
	// Forward to service manager
	err := b.serviceManager.ProcessRequest(requestMsg.Service, requestMsg.Method, clientID, requestMsg.Tracker, requestMsg.Body, requestMsg.Attributes, time.Duration(requestMsg.Timeout)*time.Millisecond)
	if err != nil {
		b.sendError(clientID, ErrorCodeInternalError, err.Error(), message.Sequence)
		return
	}
	
	b.statistics.ServiceRequests++
}

// handleServiceReply handles a service reply
func (b *Broker) handleServiceReply(clientID string, message *Message) {
	// Implementation would handle service reply
	b.statistics.ServiceReplies++
}

// handleCreditRequest handles a credit request
func (b *Broker) handleCreditRequest(clientID string, message *Message) {
	var creditMsg CreditRequestMessage
	if err := creditMsg.Unmarshal(message.Body); err != nil {
		b.sendError(clientID, ErrorCodeInvalidMessage, "Invalid credit request message", message.Sequence)
		return
	}
	
	// Process credit request
	response, err := b.creditController.RequestCredit(clientID, creditMsg.Credit, 0)
	if err != nil {
		b.sendError(clientID, ErrorCodeInternalError, err.Error(), message.Sequence)
		return
	}
	
	// Send credit confirmation
	confirmMsg := &CreditConfirmMessage{
		Credit: response.Granted,
	}
	
	body, _ := confirmMsg.Marshal()
	confirmResponse := CreateMessage(MessageTypeCreditConfirm, clientID, b.nextSequence())
	confirmResponse.Body = body
	
	b.queueResponse(clientID, confirmResponse)
}

// handleCommand processes a broker command
func (b *Broker) handleCommand(cmd brokerCmd) {
	switch cmd.action {
	case "get_statistics":
		b.mutex.RLock()
		stats := b.calculateStatistics()
		b.mutex.RUnlock()
		cmd.reply <- stats
		
	case "get_clients":
		b.mutex.RLock()
		clients := make(map[string]*Client)
		for id, client := range b.clients {
			clientCopy := *client
			clients[id] = &clientCopy
		}
		b.mutex.RUnlock()
		cmd.reply <- clients
	}
}

// Helper methods

// updateClientActivity updates a client's last activity
func (b *Broker) updateClientActivity(clientID string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	
	if client, exists := b.clients[clientID]; exists {
		client.LastActivity = time.Now()
	}
}

// removeClient removes a client
func (b *Broker) removeClient(clientID string) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	
	if _, exists := b.clients[clientID]; exists {
		delete(b.clients, clientID)
		b.statistics.ActiveConnections--
		
		// Clean up client resources
		b.creditController.RemoveClient(clientID)
		b.streamManager.RemoveClient(clientID)
		b.mailboxManager.RemoveClient(clientID)
		b.serviceManager.RemoveClient(clientID)
		
		fmt.Printf("Client %s disconnected\n", clientID)
	}
}

// sendError sends an error response to a client
func (b *Broker) sendError(clientID string, code int, reason string, sequence uint32) {
	errorResponse := CreateErrorMessage(code, reason, clientID, sequence)
	b.queueResponse(clientID, errorResponse)
}

// queueResponse queues a response to be sent to a client
func (b *Broker) queueResponse(clientID string, message *Message) {
	response := &ClientResponse{
		ClientID:  clientID,
		Message:   message,
		Timestamp: time.Now(),
	}
	
	select {
	case b.responses <- response:
		// Response queued successfully
	default:
		// Response queue full
	}
}

// sendResponse sends a response to a client
func (b *Broker) sendResponse(response *ClientResponse) {
	data, err := response.Message.Marshal()
	if err != nil {
		return
	}
	
	// Create multipart message with client ID as first frame
	msg := zmq4.NewMsgFromBytes([]byte(response.ClientID))
	msg.AppendBytes(data)
	
	if err := b.routerSocket.SendMulti(msg); err == nil {
		b.statistics.MessagesSent++
		b.statistics.BytesSent += uint64(len(data))
	}
}

// processHeartbeat processes a heartbeat event
func (b *Broker) processHeartbeat(heartbeat *HeartbeatEvent) {
	// Update client activity
	b.updateClientActivity(heartbeat.ClientID)
}

// checkClientTimeouts checks for client timeouts
func (b *Broker) checkClientTimeouts() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	
	cutoff := time.Now().Add(-b.config.ClientTimeout)
	
	for clientID, client := range b.clients {
		if client.LastActivity.Before(cutoff) {
			delete(b.clients, clientID)
			b.statistics.ActiveConnections--
			fmt.Printf("Client %s timed out\n", clientID)
		}
	}
}

// performCleanup performs periodic cleanup
func (b *Broker) performCleanup() {
	// Cleanup is handled by individual managers
}

// calculateStatistics calculates current statistics
func (b *Broker) calculateStatistics() *Statistics {
	stats := *b.statistics
	stats.ActiveConnections = uint64(len(b.clients))
	stats.LastUpdate = time.Now()
	return &stats
}

// nextSequence returns the next message sequence number
func (b *Broker) nextSequence() uint32 {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.sequenceCounter++
	return b.sequenceCounter
}