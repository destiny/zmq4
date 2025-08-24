// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package malamute

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// StreamManager manages PUB/SUB streams
type StreamManager struct {
	// Configuration
	maxStreams int           // Maximum number of streams
	retention  time.Duration // Message retention time
	
	// Stream storage
	streams   map[string]*Stream  // Active streams
	
	// Channel-based communication
	commands     chan streamCmd     // Stream control commands
	publications chan *StreamPub   // Stream publications
	
	// State management
	ctx          context.Context   // Context for cancellation
	cancel       context.CancelFunc // Cancel function
	wg           sync.WaitGroup    // Wait group for goroutines
	mutex        sync.RWMutex      // Protects stream state
}

// StreamPub represents a stream publication
type StreamPub struct {
	Stream     string            // Stream name
	Subject    string            // Message subject
	Sender     string            // Message sender
	Body       []byte            // Message body
	Attributes map[string]string // Message attributes
	Timestamp  time.Time         // Publication timestamp
}

// streamCmd represents internal stream commands
type streamCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// subscribeRequest represents a subscription request
type subscribeRequest struct {
	ClientID   string            // Subscriber client ID
	Stream     string            // Stream name
	Pattern    string            // Subject pattern
	Attributes map[string]string // Subscription attributes
}

// NewStreamManager creates a new stream manager
func NewStreamManager(maxStreams int, retention time.Duration) *StreamManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &StreamManager{
		maxStreams:   maxStreams,
		retention:    retention,
		streams:      make(map[string]*Stream),
		commands:     make(chan streamCmd, 1000),
		publications: make(chan *StreamPub, 10000),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start begins stream manager operation
func (sm *StreamManager) Start() {
	sm.wg.Add(1)
	go sm.commandLoop()
	
	sm.wg.Add(1)
	go sm.publicationLoop()
	
	sm.wg.Add(1)
	go sm.cleanupLoop()
}

// Stop stops the stream manager
func (sm *StreamManager) Stop() {
	sm.cancel()
	sm.wg.Wait()
	
	close(sm.commands)
	close(sm.publications)
}

// WriteMessage writes a message to a stream
func (sm *StreamManager) WriteMessage(streamName, subject, sender string, body []byte, attributes map[string]string) error {
	if len(streamName) == 0 || len(streamName) > MaxStreamName {
		return fmt.Errorf("invalid stream name length: %d", len(streamName))
	}
	
	if len(body) > MaxMessageSize {
		return fmt.Errorf("message too large: %d bytes", len(body))
	}
	
	publication := &StreamPub{
		Stream:     streamName,
		Subject:    subject,
		Sender:     sender,
		Body:       body,
		Attributes: attributes,
		Timestamp:  time.Now(),
	}
	
	select {
	case sm.publications <- publication:
		return nil
	case <-sm.ctx.Done():
		return sm.ctx.Err()
	default:
		return fmt.Errorf("publication queue full")
	}
}

// Subscribe subscribes a client to a stream
func (sm *StreamManager) Subscribe(clientID, streamName, pattern string, attributes map[string]string) error {
	request := &subscribeRequest{
		ClientID:   clientID,
		Stream:     streamName,
		Pattern:    pattern,
		Attributes: attributes,
	}
	
	reply := make(chan interface{}, 1)
	cmd := streamCmd{
		action: "subscribe",
		data:   request,
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-sm.ctx.Done():
		return sm.ctx.Err()
	}
}

// Unsubscribe unsubscribes a client from a stream
func (sm *StreamManager) Unsubscribe(clientID, streamName string) error {
	request := struct {
		ClientID string
		Stream   string
	}{
		ClientID: clientID,
		Stream:   streamName,
	}
	
	reply := make(chan interface{}, 1)
	cmd := streamCmd{
		action: "unsubscribe",
		data:   request,
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		<-reply
		return nil
	case <-sm.ctx.Done():
		return sm.ctx.Err()
	}
}

// RemoveClient removes a client from all subscriptions
func (sm *StreamManager) RemoveClient(clientID string) {
	reply := make(chan interface{}, 1)
	cmd := streamCmd{
		action: "remove_client",
		data:   clientID,
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		<-reply
	case <-sm.ctx.Done():
	}
}

// GetStream returns information about a stream
func (sm *StreamManager) GetStream(streamName string) (*Stream, error) {
	reply := make(chan interface{}, 1)
	cmd := streamCmd{
		action: "get_stream",
		data:   streamName,
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		result := <-reply
		if stream, ok := result.(*Stream); ok {
			return stream, nil
		}
		return nil, fmt.Errorf("stream not found: %s", streamName)
	case <-sm.ctx.Done():
		return nil, sm.ctx.Err()
	}
}

// GetStreams returns all active streams
func (sm *StreamManager) GetStreams() map[string]*Stream {
	reply := make(chan interface{}, 1)
	cmd := streamCmd{
		action: "get_streams",
		reply:  reply,
	}
	
	select {
	case sm.commands <- cmd:
		result := <-reply
		return result.(map[string]*Stream)
	case <-sm.ctx.Done():
		return make(map[string]*Stream)
	}
}

// commandLoop processes stream commands
func (sm *StreamManager) commandLoop() {
	defer sm.wg.Done()
	
	for {
		select {
		case cmd := <-sm.commands:
			sm.handleCommand(cmd)
		case <-sm.ctx.Done():
			return
		}
	}
}

// publicationLoop processes stream publications
func (sm *StreamManager) publicationLoop() {
	defer sm.wg.Done()
	
	for {
		select {
		case pub := <-sm.publications:
			sm.processPublication(pub)
		case <-sm.ctx.Done():
			return
		}
	}
}

// cleanupLoop periodically cleans up expired messages
func (sm *StreamManager) cleanupLoop() {
	defer sm.wg.Done()
	
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			sm.cleanupExpiredMessages()
		case <-sm.ctx.Done():
			return
		}
	}
}

// handleCommand processes a stream command
func (sm *StreamManager) handleCommand(cmd streamCmd) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	switch cmd.action {
	case "subscribe":
		request := cmd.data.(*subscribeRequest)
		err := sm.handleSubscribe(request)
		cmd.reply <- err
		
	case "unsubscribe":
		request := cmd.data.(struct {
			ClientID string
			Stream   string
		})
		sm.handleUnsubscribe(request.ClientID, request.Stream)
		cmd.reply <- nil
		
	case "remove_client":
		clientID := cmd.data.(string)
		sm.handleRemoveClient(clientID)
		cmd.reply <- nil
		
	case "get_stream":
		streamName := cmd.data.(string)
		if stream, exists := sm.streams[streamName]; exists {
			// Return a copy
			streamCopy := *stream
			cmd.reply <- &streamCopy
		} else {
			cmd.reply <- nil
		}
		
	case "get_streams":
		streams := make(map[string]*Stream)
		for name, stream := range sm.streams {
			streamCopy := *stream
			streams[name] = &streamCopy
		}
		cmd.reply <- streams
	}
}

// processPublication processes a stream publication
func (sm *StreamManager) processPublication(pub *StreamPub) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	// Get or create stream
	stream, exists := sm.streams[pub.Stream]
	if !exists {
		if len(sm.streams) >= sm.maxStreams {
			// Too many streams
			return
		}
		
		stream = &Stream{
			Name:         pub.Stream,
			Messages:     make([]*StreamMessage, 0),
			Subscribers:  make(map[string]*StreamSubscription),
			Attributes:   make(map[string]string),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		}
		sm.streams[pub.Stream] = stream
	}
	
	// Create message
	message := &StreamMessage{
		ID:         stream.MessageCount + 1,
		Subject:    pub.Subject,
		Sender:     pub.Sender,
		Attributes: pub.Attributes,
		Body:       pub.Body,
		Timestamp:  pub.Timestamp,
		TTL:        sm.retention,
		Priority:   0, // Default priority
	}
	
	// Add TTL from attributes if specified
	if ttlStr, exists := pub.Attributes[StreamAttrTimeToLive]; exists {
		if ttl, err := time.ParseDuration(ttlStr); err == nil {
			message.TTL = ttl
		}
	}
	
	// Store message
	stream.Messages = append(stream.Messages, message)
	stream.MessageCount++
	stream.BytesStored += uint64(len(pub.Body))
	stream.LastActivity = time.Now()
	
	// Limit message backlog
	if len(stream.Messages) > MaxStreamBacklog {
		// Remove oldest messages
		copy(stream.Messages, stream.Messages[1:])
		stream.Messages = stream.Messages[:len(stream.Messages)-1]
	}
	
	// Deliver to subscribers
	sm.deliverToSubscribers(stream, message)
}

// handleSubscribe handles a subscription request
func (sm *StreamManager) handleSubscribe(request *subscribeRequest) error {
	// Get or create stream
	stream, exists := sm.streams[request.Stream]
	if !exists {
		if len(sm.streams) >= sm.maxStreams {
			return fmt.Errorf("maximum streams exceeded")
		}
		
		stream = &Stream{
			Name:         request.Stream,
			Messages:     make([]*StreamMessage, 0),
			Subscribers:  make(map[string]*StreamSubscription),
			Attributes:   make(map[string]string),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		}
		sm.streams[request.Stream] = stream
	}
	
	// Create subscription
	subscription := &StreamSubscription{
		ClientID:   request.ClientID,
		Pattern:    request.Pattern,
		Credit:     DefaultCreditWindow, // Default credit
		Attributes: request.Attributes,
		CreatedAt:  time.Now(),
		LastRead:   stream.MessageCount, // Start from current position
	}
	
	stream.Subscribers[request.ClientID] = subscription
	
	return nil
}

// handleUnsubscribe handles an unsubscription request
func (sm *StreamManager) handleUnsubscribe(clientID, streamName string) {
	if stream, exists := sm.streams[streamName]; exists {
		delete(stream.Subscribers, clientID)
		
		// Remove stream if no subscribers and no recent activity
		if len(stream.Subscribers) == 0 && time.Since(stream.LastActivity) > time.Hour {
			delete(sm.streams, streamName)
		}
	}
}

// handleRemoveClient removes a client from all subscriptions
func (sm *StreamManager) handleRemoveClient(clientID string) {
	for streamName, stream := range sm.streams {
		delete(stream.Subscribers, clientID)
		
		// Remove stream if no subscribers and no recent activity
		if len(stream.Subscribers) == 0 && time.Since(stream.LastActivity) > time.Hour {
			delete(sm.streams, streamName)
		}
	}
}

// deliverToSubscribers delivers a message to matching subscribers
func (sm *StreamManager) deliverToSubscribers(stream *Stream, message *StreamMessage) {
	for clientID, subscription := range stream.Subscribers {
		// Check if subject matches pattern
		if sm.matchesPattern(message.Subject, subscription.Pattern) {
			// Check credit
			if subscription.Credit > 0 {
				// Deliver message (would send via broker response channel)
				subscription.Credit--
				subscription.LastRead = message.ID
				
				// In a real implementation, this would send a StreamSendMessage
				// to the broker's response channel for delivery to the client
				fmt.Printf("Delivering message %d from stream '%s' to client '%s'\n", 
					message.ID, stream.Name, clientID)
			}
		}
	}
}

// matchesPattern checks if a subject matches a subscription pattern
func (sm *StreamManager) matchesPattern(subject, pattern string) bool {
	// Simple pattern matching - supports wildcards
	if pattern == "*" {
		return true
	}
	
	// Support glob-style patterns
	matched, err := filepath.Match(pattern, subject)
	if err != nil {
		// Fallback to exact match
		return subject == pattern
	}
	
	return matched
}

// cleanupExpiredMessages removes expired messages from streams
func (sm *StreamManager) cleanupExpiredMessages() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	now := time.Now()
	
	for streamName, stream := range sm.streams {
		// Remove expired messages
		validMessages := make([]*StreamMessage, 0, len(stream.Messages))
		
		for _, message := range stream.Messages {
			if message.TTL > 0 && now.Sub(message.Timestamp) > message.TTL {
				// Message expired
				stream.BytesStored -= uint64(len(message.Body))
			} else {
				validMessages = append(validMessages, message)
			}
		}
		
		stream.Messages = validMessages
		
		// Remove inactive streams with no subscribers
		if len(stream.Subscribers) == 0 && 
		   len(stream.Messages) == 0 && 
		   time.Since(stream.LastActivity) > 24*time.Hour {
			delete(sm.streams, streamName)
		}
	}
}

// RestoreCredit restores credit to a subscription
func (sm *StreamManager) RestoreCredit(clientID, streamName string, credit int32) error {
	reply := make(chan interface{}, 1)
	cmd := streamCmd{
		action: "restore_credit",
		data: struct {
			ClientID string
			Stream   string
			Credit   int32
		}{
			ClientID: clientID,
			Stream:   streamName,
			Credit:   credit,
		},
		reply: reply,
	}
	
	select {
	case sm.commands <- cmd:
		<-reply
		return nil
	case <-sm.ctx.Done():
		return sm.ctx.Err()
	}
}