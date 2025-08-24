// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dafka

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/destiny/zmq4/v25"
)

// Consumer represents a DAFKA consumer with channel-based record delivery
type Consumer struct {
	// Configuration
	config    *ConsumerConfig  // Consumer configuration
	uuid      string           // Consumer UUID
	groupID   string           // Consumer group ID
	
	// Network components
	subSocket   zmq4.Socket     // SUB socket for receiving records
	dealerSocket zmq4.Socket    // DEALER socket for direct communication
	towerSocket zmq4.Socket     // Socket for tower communication
	
	// Subscription management
	subscriptions map[string]*Subscription // Topic subscriptions
	partitions    map[string]*PartitionState // Partition consumption state
	
	// Channel-based communication
	records      chan *Record         // Incoming records
	commands     chan consumerCmd     // Consumer control commands
	fetchRequests chan *FetchRequest  // Fetch requests for missing records
	acks         chan *AckMessage     // Acknowledgments to send
	commits      chan *CommitRequest  // Offset commit requests
	
	// State management
	state       int             // Consumer state
	ctx         context.Context // Context for cancellation
	cancel      context.CancelFunc // Cancel function
	wg          sync.WaitGroup  // Wait group for goroutines
	mutex       sync.RWMutex    // Protects consumer state
	statistics  *Statistics    // Consumer statistics
}

// PartitionState tracks consumption state for a partition
type PartitionState struct {
	Topic         string    // Topic name
	Partition     string    // Partition identifier
	LastOffset    uint64    // Last consumed offset
	CommittedOffset uint64  // Last committed offset
	HighWatermark uint64    // Highest known offset
	LastFetch     time.Time // When partition was last fetched
	Lagging       bool      // Whether partition is lagging
}

// FetchRequest represents a request to fetch records from a partition
type FetchRequest struct {
	Topic     string    // Topic name
	Partition string    // Partition identifier
	Offset    uint64    // Starting offset
	Count     uint32    // Number of records to fetch
	Response  chan *FetchResponse // Response channel
}

// FetchResponse represents the response to a fetch request
type FetchResponse struct {
	Topic     string    // Topic name
	Partition string    // Partition identifier
	Records   []*Record // Fetched records
	Error     error     // Error if fetch failed
}

// CommitRequest represents a request to commit offsets
type CommitRequest struct {
	Offsets  map[string]map[string]uint64 // Topic -> Partition -> Offset
	Response chan *CommitResponse         // Response channel
}

// CommitResponse represents the response to a commit request
type CommitResponse struct {
	Error error // Error if commit failed
}

// consumerCmd represents internal consumer commands
type consumerCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// RecordHandler is a function type for processing consumed records
type RecordHandler func(*Record) error

// NewConsumer creates a new DAFKA consumer
func NewConsumer(config *ConsumerConfig) (*Consumer, error) {
	if config == nil {
		return nil, fmt.Errorf("consumer config is required")
	}
	
	if config.NodeUUID == "" {
		config.NodeUUID = generateUUID()
	}
	
	if config.Address == "" {
		config.Address = "tcp://*:0" // Auto-assign port
	}
	
	if config.GroupID == "" {
		config.GroupID = "default"
	}
	
	if config.FetchSize <= 0 {
		config.FetchSize = 1000
	}
	
	if config.CommitIntervalMs <= 0 {
		config.CommitIntervalMs = 5000
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	consumer := &Consumer{
		config:        config,
		uuid:          config.NodeUUID,
		groupID:       config.GroupID,
		subscriptions: make(map[string]*Subscription),
		partitions:    make(map[string]*PartitionState),
		records:       make(chan *Record, 10000),
		commands:      make(chan consumerCmd, 100),
		fetchRequests: make(chan *FetchRequest, 1000),
		acks:          make(chan *AckMessage, 10000),
		commits:       make(chan *CommitRequest, 100),
		state:         NodeStateStopped,
		ctx:           ctx,
		cancel:        cancel,
		statistics:    &Statistics{},
	}
	
	return consumer, nil
}

// Start begins consumer operation
func (c *Consumer) Start() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	if c.state != NodeStateStopped {
		return fmt.Errorf("consumer already started")
	}
	
	c.state = NodeStateStarting
	
	// Create and bind SUB socket for receiving records
	c.subSocket = zmq4.NewSub(c.ctx)
	
	// Create DEALER socket for direct communication
	c.dealerSocket = zmq4.NewDealer(c.ctx)
	if err := c.dealerSocket.Listen(c.config.Address); err != nil {
		return fmt.Errorf("failed to bind DEALER socket: %w", err)
	}
	
	// Connect to tower if specified
	if c.config.TowerAddr != "" {
		c.towerSocket = zmq4.NewDealer(c.ctx)
		if err := c.towerSocket.Dial(c.config.TowerAddr); err != nil {
			return fmt.Errorf("failed to connect to tower: %w", err)
		}
	}
	
	// Start internal loops
	c.wg.Add(1)
	go c.commandLoop()
	
	c.wg.Add(1)
	go c.subscriptionLoop()
	
	c.wg.Add(1)
	go c.fetchLoop()
	
	c.wg.Add(1)
	go c.ackLoop()
	
	c.wg.Add(1)
	go c.commitLoop()
	
	if c.config.AutoCommit {
		c.wg.Add(1)
		go c.autoCommitLoop()
	}
	
	c.state = NodeStateRunning
	c.statistics.StartTime = time.Now()
	
	return nil
}

// Stop stops the consumer
func (c *Consumer) Stop() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	if c.state == NodeStateStopped {
		return
	}
	
	c.state = NodeStateStopping
	
	// Commit pending offsets
	if c.config.AutoCommit {
		c.commitOffsets()
	}
	
	// Cancel context and wait for goroutines
	c.cancel()
	c.wg.Wait()
	
	// Close sockets
	if c.subSocket != nil {
		c.subSocket.Close()
	}
	if c.dealerSocket != nil {
		c.dealerSocket.Close()
	}
	if c.towerSocket != nil {
		c.towerSocket.Close()
	}
	
	close(c.records)
	close(c.commands)
	close(c.fetchRequests)
	close(c.acks)
	close(c.commits)
	
	c.state = NodeStateStopped
}

// Subscribe subscribes to one or more topics
func (c *Consumer) Subscribe(topics ...string) error {
	reply := make(chan interface{}, 1)
	cmd := consumerCmd{
		action: "subscribe",
		data:   topics,
		reply:  reply,
	}
	
	select {
	case c.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

// Unsubscribe unsubscribes from one or more topics
func (c *Consumer) Unsubscribe(topics ...string) error {
	reply := make(chan interface{}, 1)
	cmd := consumerCmd{
		action: "unsubscribe",
		data:   topics,
		reply:  reply,
	}
	
	select {
	case c.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

// Poll polls for records with timeout
func (c *Consumer) Poll(timeout time.Duration) (*Record, error) {
	select {
	case record := <-c.records:
		// Update partition state
		c.updatePartitionState(record)
		return record, nil
	case <-time.After(timeout):
		return nil, nil // No records available
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

// Records returns the channel for receiving records
func (c *Consumer) Records() <-chan *Record {
	return c.records
}

// Consume starts consuming records with a handler function
func (c *Consumer) Consume(handler RecordHandler) error {
	for {
		select {
		case record := <-c.records:
			// Update partition state
			c.updatePartitionState(record)
			
			// Process record with handler
			if err := handler(record); err != nil {
				c.statistics.ConsumeErrors++
				return fmt.Errorf("handler error: %w", err)
			}
			
			// Send acknowledgment if auto-commit is disabled
			if !c.config.AutoCommit {
				ack := &AckMessage{
					Partition: record.Partition,
					Offset:    record.Offset,
				}
				select {
				case c.acks <- ack:
				default:
					// ACK queue full
				}
			}
			
		case <-c.ctx.Done():
			return c.ctx.Err()
		}
	}
}

// Commit commits current offsets
func (c *Consumer) Commit() error {
	response := make(chan *CommitResponse, 1)
	
	// Get current offsets
	offsets := make(map[string]map[string]uint64)
	c.mutex.RLock()
	for partitionKey, partitionState := range c.partitions {
		topicOffsets, exists := offsets[partitionState.Topic]
		if !exists {
			topicOffsets = make(map[string]uint64)
			offsets[partitionState.Topic] = topicOffsets
		}
		topicOffsets[partitionState.Partition] = partitionState.LastOffset
	}
	c.mutex.RUnlock()
	
	request := &CommitRequest{
		Offsets:  offsets,
		Response: response,
	}
	
	select {
	case c.commits <- request:
		// Request queued successfully
	case <-c.ctx.Done():
		return c.ctx.Err()
	default:
		return fmt.Errorf("commit queue full")
	}
	
	// Wait for response
	select {
	case resp := <-response:
		return resp.Error
	case <-c.ctx.Done():
		return c.ctx.Err()
	case <-time.After(DefaultStoreTimeout):
		return fmt.Errorf("commit timeout")
	}
}

// GetStatistics returns consumer statistics
func (c *Consumer) GetStatistics() *Statistics {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	stats := *c.statistics
	stats.LastUpdate = time.Now()
	return &stats
}

// commandLoop processes consumer commands
func (c *Consumer) commandLoop() {
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

// subscriptionLoop processes incoming subscription messages
func (c *Consumer) subscriptionLoop() {
	defer c.wg.Done()
	
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			// Receive messages from SUB socket
			msg, err := c.subSocket.Recv()
			if err != nil {
				continue
			}
			
			c.processSubscriptionMessage(msg.Bytes())
		}
	}
}

// fetchLoop processes fetch requests
func (c *Consumer) fetchLoop() {
	defer c.wg.Done()
	
	for {
		select {
		case request := <-c.fetchRequests:
			c.processFetchRequest(request)
		case <-c.ctx.Done():
			return
		}
	}
}

// ackLoop processes acknowledgment messages
func (c *Consumer) ackLoop() {
	defer c.wg.Done()
	
	for {
		select {
		case ack := <-c.acks:
			c.processAck(ack)
		case <-c.ctx.Done():
			return
		}
	}
}

// commitLoop processes commit requests
func (c *Consumer) commitLoop() {
	defer c.wg.Done()
	
	for {
		select {
		case request := <-c.commits:
			c.processCommitRequest(request)
		case <-c.ctx.Done():
			return
		}
	}
}

// autoCommitLoop automatically commits offsets at regular intervals
func (c *Consumer) autoCommitLoop() {
	defer c.wg.Done()
	
	ticker := time.NewTicker(time.Duration(c.config.CommitIntervalMs) * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			c.commitOffsets()
		case <-c.ctx.Done():
			return
		}
	}
}

// handleCommand processes a consumer command
func (c *Consumer) handleCommand(cmd consumerCmd) {
	switch cmd.action {
	case "subscribe":
		topics := cmd.data.([]string)
		err := c.subscribeToTopics(topics)
		cmd.reply <- err
		
	case "unsubscribe":
		topics := cmd.data.([]string)
		err := c.unsubscribeFromTopics(topics)
		cmd.reply <- err
		
	case "get_subscriptions":
		c.mutex.RLock()
		topics := make([]string, 0, len(c.subscriptions))
		for topic := range c.subscriptions {
			topics = append(topics, topic)
		}
		c.mutex.RUnlock()
		cmd.reply <- topics
	}
}

// subscribeToTopics subscribes to the specified topics
func (c *Consumer) subscribeToTopics(topics []string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	for _, topic := range topics {
		// Create subscription
		subscription := &Subscription{
			Topic:      topic,
			Partitions: make(map[string]bool),
			Offset:     make(map[string]uint64),
			Headers:    c.config.Headers,
			StartTime:  time.Now(),
		}
		
		c.subscriptions[topic] = subscription
		
		// Subscribe to topic on SUB socket
		c.subSocket.SetOption(zmq4.OptionSubscribe, topic)
	}
	
	return nil
}

// unsubscribeFromTopics unsubscribes from the specified topics
func (c *Consumer) unsubscribeFromTopics(topics []string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	for _, topic := range topics {
		// Remove subscription
		delete(c.subscriptions, topic)
		
		// Unsubscribe from topic on SUB socket
		c.subSocket.SetOption(zmq4.OptionUnsubscribe, topic)
		
		// Clean up partition states for this topic
		for partitionKey, partitionState := range c.partitions {
			if partitionState.Topic == topic {
				delete(c.partitions, partitionKey)
			}
		}
	}
	
	return nil
}

// processSubscriptionMessage processes an incoming subscription message
func (c *Consumer) processSubscriptionMessage(data []byte) {
	// Parse DAFKA message
	var msg Message
	if err := msg.Unmarshal(data); err != nil {
		c.statistics.ConsumeErrors++
		return
	}
	
	// Check if we're subscribed to this topic
	c.mutex.RLock()
	_, subscribed := c.subscriptions[msg.Topic]
	c.mutex.RUnlock()
	
	if !subscribed {
		return
	}
	
	// Process different message types
	switch msg.Type {
	case MessageTypeRecord:
		c.processRecordMessage(&msg)
	case MessageTypeHead:
		c.processHeadMessage(&msg)
	default:
		// Ignore other message types
	}
	
	c.statistics.MessagesIn++
	c.statistics.BytesIn += uint64(len(data))
}

// processRecordMessage processes a RECORD message
func (c *Consumer) processRecordMessage(msg *Message) {
	var recordMsg RecordMessage
	if err := recordMsg.Unmarshal(msg.Body); err != nil {
		c.statistics.ConsumeErrors++
		return
	}
	
	// Create record
	record := &Record{
		Topic:     msg.Topic,
		Partition: recordMsg.Partition,
		Offset:    recordMsg.Offset,
		Key:       recordMsg.Key,
		Value:     recordMsg.Value,
		Headers:   recordMsg.Headers,
		Timestamp: time.Now(), // Use current time for consumption
	}
	
	// Check if this is the next expected record
	partitionKey := fmt.Sprintf("%s:%s", msg.Topic, recordMsg.Partition)
	c.mutex.RLock()
	partitionState, exists := c.partitions[partitionKey]
	expectedOffset := uint64(0)
	if exists {
		expectedOffset = partitionState.LastOffset + 1
	}
	c.mutex.RUnlock()
	
	if record.Offset != expectedOffset {
		// Missing records - request fetch
		c.requestMissingRecords(msg.Topic, recordMsg.Partition, expectedOffset, record.Offset)
	}
	
	// Deliver record to consumer
	select {
	case c.records <- record:
		// Record delivered successfully
		c.statistics.RecordsConsumed++
		c.statistics.BytesConsumed += uint64(len(record.Key) + len(record.Value))
	default:
		// Record queue full - drop record
		c.statistics.ConsumeErrors++
	}
}

// processHeadMessage processes a HEAD message
func (c *Consumer) processHeadMessage(msg *Message) {
	var headMsg HeadMessage
	if err := headMsg.Unmarshal(msg.Body); err != nil {
		c.statistics.ConsumeErrors++
		return
	}
	
	// Update partition high watermark
	partitionKey := fmt.Sprintf("%s:%s", msg.Topic, headMsg.Partition)
	c.mutex.Lock()
	partitionState, exists := c.partitions[partitionKey]
	if exists {
		partitionState.HighWatermark = headMsg.Offset
		partitionState.Lagging = partitionState.LastOffset < headMsg.Offset
	}
	c.mutex.Unlock()
}

// updatePartitionState updates the state for a consumed record
func (c *Consumer) updatePartitionState(record *Record) {
	partitionKey := fmt.Sprintf("%s:%s", record.Topic, record.Partition)
	
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	partitionState, exists := c.partitions[partitionKey]
	if !exists {
		partitionState = &PartitionState{
			Topic:         record.Topic,
			Partition:     record.Partition,
			LastOffset:    record.Offset,
			CommittedOffset: 0,
			HighWatermark: record.Offset,
			LastFetch:     time.Now(),
		}
		c.partitions[partitionKey] = partitionState
	} else {
		partitionState.LastOffset = record.Offset
		partitionState.LastFetch = time.Now()
	}
}

// requestMissingRecords requests missing records from a partition
func (c *Consumer) requestMissingRecords(topic, partition string, startOffset, endOffset uint64) {
	if startOffset >= endOffset {
		return
	}
	
	count := uint32(endOffset - startOffset)
	if count > uint32(c.config.FetchSize) {
		count = uint32(c.config.FetchSize)
	}
	
	response := make(chan *FetchResponse, 1)
	request := &FetchRequest{
		Topic:     topic,
		Partition: partition,
		Offset:    startOffset,
		Count:     count,
		Response:  response,
	}
	
	select {
	case c.fetchRequests <- request:
		// Request queued successfully
	default:
		// Fetch queue full
	}
}

// processFetchRequest processes a fetch request
func (c *Consumer) processFetchRequest(request *FetchRequest) {
	// Create FETCH message
	msg, err := CreateFetchMessage(request.Topic, request.Partition, request.Offset, request.Count)
	if err != nil {
		request.Response <- &FetchResponse{Error: err}
		return
	}
	
	// Send fetch request via DEALER socket
	data, err := msg.Marshal()
	if err != nil {
		request.Response <- &FetchResponse{Error: err}
		return
	}
	
	zmqMsg := zmq4.NewMsgFromBytes(data)
	if err := c.dealerSocket.SendMulti(zmqMsg); err != nil {
		request.Response <- &FetchResponse{Error: err}
		return
	}
	
	// For now, send empty response (actual implementation would wait for response)
	request.Response <- &FetchResponse{
		Topic:     request.Topic,
		Partition: request.Partition,
		Records:   []*Record{},
	}
}

// processAck processes an acknowledgment
func (c *Consumer) processAck(ack *AckMessage) {
	// Send ACK message (implementation would send via appropriate socket)
	_ = ack
}

// processCommitRequest processes a commit request
func (c *Consumer) processCommitRequest(request *CommitRequest) {
	// Update committed offsets
	c.mutex.Lock()
	for topic, topicOffsets := range request.Offsets {
		for partition, offset := range topicOffsets {
			partitionKey := fmt.Sprintf("%s:%s", topic, partition)
			if partitionState, exists := c.partitions[partitionKey]; exists {
				partitionState.CommittedOffset = offset
			}
		}
	}
	c.mutex.Unlock()
	
	// Send response
	request.Response <- &CommitResponse{}
}

// commitOffsets commits current offsets for all partitions
func (c *Consumer) commitOffsets() {
	offsets := make(map[string]map[string]uint64)
	
	c.mutex.RLock()
	for partitionKey, partitionState := range c.partitions {
		topicOffsets, exists := offsets[partitionState.Topic]
		if !exists {
			topicOffsets = make(map[string]uint64)
			offsets[partitionState.Topic] = topicOffsets
		}
		topicOffsets[partitionState.Partition] = partitionState.LastOffset
	}
	c.mutex.RUnlock()
	
	if len(offsets) == 0 {
		return
	}
	
	response := make(chan *CommitResponse, 1)
	request := &CommitRequest{
		Offsets:  offsets,
		Response: response,
	}
	
	select {
	case c.commits <- request:
		// Wait for response
		<-response
	default:
		// Commit queue full
	}
}