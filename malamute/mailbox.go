// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package malamute

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MailboxManager manages direct peer-to-peer messaging
type MailboxManager struct {
	// Configuration
	maxMailboxes int           // Maximum number of mailboxes
	retention    time.Duration // Message retention time
	
	// Mailbox storage
	mailboxes map[string]*Mailbox // Active mailboxes
	
	// Channel-based communication
	commands    chan mailboxCmd     // Mailbox control commands
	deliveries  chan *MailboxDelivery // Message deliveries
	
	// State management
	ctx         context.Context   // Context for cancellation
	cancel      context.CancelFunc // Cancel function
	wg          sync.WaitGroup    // Wait group for goroutines
	mutex       sync.RWMutex      // Protects mailbox state
	messageID   uint64            // Message ID counter
}

// MailboxDelivery represents a message delivery
type MailboxDelivery struct {
	Address    string            // Target mailbox address
	Subject    string            // Message subject
	Sender     string            // Message sender
	Tracker    string            // Message tracker
	Body       []byte            // Message body
	Attributes map[string]string // Message attributes
	Timeout    time.Duration     // Message timeout
	Timestamp  time.Time         // Delivery timestamp
}

// mailboxCmd represents internal mailbox commands
type mailboxCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// receiveRequest represents a receive request
type receiveRequest struct {
	ClientID   string            // Requesting client ID
	Address    string            // Mailbox address
	Attributes map[string]string // Request attributes
}

// NewMailboxManager creates a new mailbox manager
func NewMailboxManager(maxMailboxes int, retention time.Duration) *MailboxManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &MailboxManager{
		maxMailboxes: maxMailboxes,
		retention:    retention,
		mailboxes:    make(map[string]*Mailbox),
		commands:     make(chan mailboxCmd, 1000),
		deliveries:   make(chan *MailboxDelivery, 10000),
		ctx:          ctx,
		cancel:       cancel,
		messageID:    0,
	}
}

// Start begins mailbox manager operation
func (mm *MailboxManager) Start() {
	mm.wg.Add(1)
	go mm.commandLoop()
	
	mm.wg.Add(1)
	go mm.deliveryLoop()
	
	mm.wg.Add(1)
	go mm.cleanupLoop()
}

// Stop stops the mailbox manager
func (mm *MailboxManager) Stop() {
	mm.cancel()
	mm.wg.Wait()
	
	close(mm.commands)
	close(mm.deliveries)
}

// SendMessage sends a message to a mailbox
func (mm *MailboxManager) SendMessage(address, subject, sender, tracker string, body []byte, attributes map[string]string, timeout time.Duration) error {
	if len(address) == 0 || len(address) > MaxMailboxName {
		return fmt.Errorf("invalid mailbox address length: %d", len(address))
	}
	
	if len(body) > MaxMessageSize {
		return fmt.Errorf("message too large: %d bytes", len(body))
	}
	
	delivery := &MailboxDelivery{
		Address:    address,
		Subject:    subject,
		Sender:     sender,
		Tracker:    tracker,
		Body:       body,
		Attributes: attributes,
		Timeout:    timeout,
		Timestamp:  time.Now(),
	}
	
	select {
	case mm.deliveries <- delivery:
		return nil
	case <-mm.ctx.Done():
		return mm.ctx.Err()
	default:
		return fmt.Errorf("delivery queue full")
	}
}

// ReceiveMessage attempts to receive a message from a mailbox
func (mm *MailboxManager) ReceiveMessage(clientID, address string, attributes map[string]string) (*MailboxMessage, error) {
	request := &receiveRequest{
		ClientID:   clientID,
		Address:    address,
		Attributes: attributes,
	}
	
	reply := make(chan interface{}, 1)
	cmd := mailboxCmd{
		action: "receive",
		data:   request,
		reply:  reply,
	}
	
	select {
	case mm.commands <- cmd:
		result := <-reply
		if message, ok := result.(*MailboxMessage); ok {
			return message, nil
		}
		if err, ok := result.(error); ok {
			return nil, err
		}
		return nil, nil // No message available
	case <-mm.ctx.Done():
		return nil, mm.ctx.Err()
	}
}

// CreateMailbox creates a new mailbox for a client
func (mm *MailboxManager) CreateMailbox(address, owner string, attributes map[string]string) error {
	request := struct {
		Address    string
		Owner      string
		Attributes map[string]string
	}{
		Address:    address,
		Owner:      owner,
		Attributes: attributes,
	}
	
	reply := make(chan interface{}, 1)
	cmd := mailboxCmd{
		action: "create",
		data:   request,
		reply:  reply,
	}
	
	select {
	case mm.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-mm.ctx.Done():
		return mm.ctx.Err()
	}
}

// RemoveClient removes a client's mailboxes
func (mm *MailboxManager) RemoveClient(clientID string) {
	reply := make(chan interface{}, 1)
	cmd := mailboxCmd{
		action: "remove_client",
		data:   clientID,
		reply:  reply,
	}
	
	select {
	case mm.commands <- cmd:
		<-reply
	case <-mm.ctx.Done():
	}
}

// GetMailbox returns information about a mailbox
func (mm *MailboxManager) GetMailbox(address string) (*Mailbox, error) {
	reply := make(chan interface{}, 1)
	cmd := mailboxCmd{
		action: "get_mailbox",
		data:   address,
		reply:  reply,
	}
	
	select {
	case mm.commands <- cmd:
		result := <-reply
		if mailbox, ok := result.(*Mailbox); ok {
			return mailbox, nil
		}
		return nil, fmt.Errorf("mailbox not found: %s", address)
	case <-mm.ctx.Done():
		return nil, mm.ctx.Err()
	}
}

// GetMailboxes returns all active mailboxes
func (mm *MailboxManager) GetMailboxes() map[string]*Mailbox {
	reply := make(chan interface{}, 1)
	cmd := mailboxCmd{
		action: "get_mailboxes",
		reply:  reply,
	}
	
	select {
	case mm.commands <- cmd:
		result := <-reply
		return result.(map[string]*Mailbox)
	case <-mm.ctx.Done():
		return make(map[string]*Mailbox)
	}
}

// commandLoop processes mailbox commands
func (mm *MailboxManager) commandLoop() {
	defer mm.wg.Done()
	
	for {
		select {
		case cmd := <-mm.commands:
			mm.handleCommand(cmd)
		case <-mm.ctx.Done():
			return
		}
	}
}

// deliveryLoop processes message deliveries
func (mm *MailboxManager) deliveryLoop() {
	defer mm.wg.Done()
	
	for {
		select {
		case delivery := <-mm.deliveries:
			mm.processDelivery(delivery)
		case <-mm.ctx.Done():
			return
		}
	}
}

// cleanupLoop periodically cleans up expired messages
func (mm *MailboxManager) cleanupLoop() {
	defer mm.wg.Done()
	
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			mm.cleanupExpiredMessages()
		case <-mm.ctx.Done():
			return
		}
	}
}

// handleCommand processes a mailbox command
func (mm *MailboxManager) handleCommand(cmd mailboxCmd) {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()
	
	switch cmd.action {
	case "receive":
		request := cmd.data.(*receiveRequest)
		message := mm.handleReceive(request)
		cmd.reply <- message
		
	case "create":
		request := cmd.data.(struct {
			Address    string
			Owner      string
			Attributes map[string]string
		})
		err := mm.handleCreate(request.Address, request.Owner, request.Attributes)
		cmd.reply <- err
		
	case "remove_client":
		clientID := cmd.data.(string)
		mm.handleRemoveClient(clientID)
		cmd.reply <- nil
		
	case "get_mailbox":
		address := cmd.data.(string)
		if mailbox, exists := mm.mailboxes[address]; exists {
			// Return a copy
			mailboxCopy := *mailbox
			cmd.reply <- &mailboxCopy
		} else {
			cmd.reply <- nil
		}
		
	case "get_mailboxes":
		mailboxes := make(map[string]*Mailbox)
		for address, mailbox := range mm.mailboxes {
			mailboxCopy := *mailbox
			mailboxes[address] = &mailboxCopy
		}
		cmd.reply <- mailboxes
	}
}

// processDelivery processes a message delivery
func (mm *MailboxManager) processDelivery(delivery *MailboxDelivery) {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()
	
	// Get or create mailbox
	mailbox, exists := mm.mailboxes[delivery.Address]
	if !exists {
		if len(mm.mailboxes) >= mm.maxMailboxes {
			// Too many mailboxes
			return
		}
		
		mailbox = &Mailbox{
			Address:      delivery.Address,
			Messages:     make([]*MailboxMessage, 0),
			Owner:        "", // Will be set when client connects
			Attributes:   make(map[string]string),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		}
		mm.mailboxes[delivery.Address] = mailbox
	}
	
	// Create message
	mm.messageID++
	message := &MailboxMessage{
		ID:         mm.messageID,
		Subject:    delivery.Subject,
		Sender:     delivery.Sender,
		Tracker:    delivery.Tracker,
		Attributes: delivery.Attributes,
		Body:       delivery.Body,
		Timestamp:  delivery.Timestamp,
		Timeout:    delivery.Timestamp.Add(delivery.Timeout),
		Retries:    0,
	}
	
	// Store message
	mailbox.Messages = append(mailbox.Messages, message)
	mailbox.MessageCount++
	mailbox.BytesStored += uint64(len(delivery.Body))
	mailbox.LastActivity = time.Now()
	
	// Limit mailbox backlog
	if len(mailbox.Messages) > MaxMailboxBacklog {
		// Remove oldest messages
		removed := mailbox.Messages[0]
		mailbox.BytesStored -= uint64(len(removed.Body))
		copy(mailbox.Messages, mailbox.Messages[1:])
		mailbox.Messages = mailbox.Messages[:len(mailbox.Messages)-1]
	}
	
	// In a real implementation, this would notify waiting clients
	fmt.Printf("Message %d delivered to mailbox '%s' from '%s'\n", 
		message.ID, mailbox.Address, delivery.Sender)
}

// handleReceive handles a message receive request
func (mm *MailboxManager) handleReceive(request *receiveRequest) interface{} {
	mailbox, exists := mm.mailboxes[request.Address]
	if !exists {
		return fmt.Errorf("mailbox not found: %s", request.Address)
	}
	
	// Check if client owns this mailbox
	if mailbox.Owner != "" && mailbox.Owner != request.ClientID {
		return fmt.Errorf("access denied to mailbox: %s", request.Address)
	}
	
	// Set owner if not set
	if mailbox.Owner == "" {
		mailbox.Owner = request.ClientID
	}
	
	// Find first non-expired message
	now := time.Now()
	for i, message := range mailbox.Messages {
		if message.Timeout.After(now) {
			// Remove message from mailbox
			copy(mailbox.Messages[i:], mailbox.Messages[i+1:])
			mailbox.Messages = mailbox.Messages[:len(mailbox.Messages)-1]
			mailbox.BytesStored -= uint64(len(message.Body))
			mailbox.LastActivity = time.Now()
			
			return message
		}
	}
	
	return nil // No message available
}

// handleCreate handles a mailbox creation request
func (mm *MailboxManager) handleCreate(address, owner string, attributes map[string]string) error {
	if len(mm.mailboxes) >= mm.maxMailboxes {
		return fmt.Errorf("maximum mailboxes exceeded")
	}
	
	if _, exists := mm.mailboxes[address]; exists {
		return fmt.Errorf("mailbox already exists: %s", address)
	}
	
	mailbox := &Mailbox{
		Address:      address,
		Messages:     make([]*MailboxMessage, 0),
		Owner:        owner,
		Attributes:   attributes,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}
	
	mm.mailboxes[address] = mailbox
	
	return nil
}

// handleRemoveClient removes all mailboxes owned by a client
func (mm *MailboxManager) handleRemoveClient(clientID string) {
	for address, mailbox := range mm.mailboxes {
		if mailbox.Owner == clientID {
			delete(mm.mailboxes, address)
		}
	}
}

// cleanupExpiredMessages removes expired messages from mailboxes
func (mm *MailboxManager) cleanupExpiredMessages() {
	mm.mutex.Lock()
	defer mm.mutex.Unlock()
	
	now := time.Now()
	
	for address, mailbox := range mm.mailboxes {
		// Remove expired messages
		validMessages := make([]*MailboxMessage, 0, len(mailbox.Messages))
		
		for _, message := range mailbox.Messages {
			if message.Timeout.After(now) {
				validMessages = append(validMessages, message)
			} else {
				// Message expired
				mailbox.BytesStored -= uint64(len(message.Body))
			}
		}
		
		mailbox.Messages = validMessages
		
		// Remove empty mailboxes with no recent activity
		if len(mailbox.Messages) == 0 && 
		   time.Since(mailbox.LastActivity) > mm.retention {
			delete(mm.mailboxes, address)
		}
	}
}

// GetMessageCount returns the number of messages in a mailbox
func (mm *MailboxManager) GetMessageCount(address string) (int, error) {
	reply := make(chan interface{}, 1)
	cmd := mailboxCmd{
		action: "get_message_count",
		data:   address,
		reply:  reply,
	}
	
	select {
	case mm.commands <- cmd:
		result := <-reply
		if count, ok := result.(int); ok {
			return count, nil
		}
		return 0, fmt.Errorf("mailbox not found: %s", address)
	case <-mm.ctx.Done():
		return 0, mm.ctx.Err()
	}
}

// DeliverMessage delivers a message directly (for internal use)
func (mm *MailboxManager) DeliverMessage(clientID string, message *MailboxMessage) {
	// In a real implementation, this would send a MailboxDeliverMessage
	// to the broker's response channel for delivery to the client
	fmt.Printf("Delivering mailbox message %d to client '%s'\n", 
		message.ID, clientID)
}