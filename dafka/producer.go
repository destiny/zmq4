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

// Producer represents a DAFKA producer with channel-based record publishing
type Producer struct {
	// Configuration
	config    *ProducerConfig // Producer configuration
	uuid      string          // Producer UUID (also partition identifier)
	
	// Network components
	pubSocket  zmq4.Socket     // PUB socket for publishing records
	towerSocket zmq4.Socket    // Socket for tower communication
	
	// Record management
	topics    map[string]*TopicState // Topic state management
	records   chan *ProduceRequest   // Incoming record requests
	batches   chan *RecordBatch      // Batched records for publishing
	
	// Channel-based communication
	commands     chan producerCmd     // Producer control commands
	responses    chan *ProduceResponse // Produce response notifications
	acks         chan *AckMessage     // Acknowledgment messages
	
	// State management
	state      int             // Producer state
	ctx        context.Context // Context for cancellation
	cancel     context.CancelFunc // Cancel function
	wg         sync.WaitGroup  // Wait group for goroutines
	mutex      sync.RWMutex    // Protects producer state
	statistics *Statistics    // Producer statistics
}

// TopicState tracks state for a specific topic
type TopicState struct {
	Name         string            // Topic name
	NextOffset   uint64            // Next offset to assign
	PendingBatch *RecordBatch      // Current batch being accumulated
	LastFlush    time.Time         // When batch was last flushed
	Headers      map[string]string // Topic-level headers
}

// ProduceRequest represents a request to publish a record
type ProduceRequest struct {
	Topic     string            // Topic name
	Key       []byte            // Record key (optional)
	Value     []byte            // Record value
	Headers   map[string]string // Record headers
	Timestamp time.Time         // Record timestamp
	Response  chan *ProduceResponse // Response channel
}

// ProduceResponse represents the response to a produce request
type ProduceResponse struct {
	Topic     string    // Topic name
	Partition string    // Partition identifier
	Offset    uint64    // Assigned offset
	Timestamp time.Time // When record was published
	Error     error     // Error if publish failed
}

// producerCmd represents internal producer commands
type producerCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// NewProducer creates a new DAFKA producer
func NewProducer(config *ProducerConfig) (*Producer, error) {
	if config == nil {
		return nil, fmt.Errorf("producer config is required")
	}
	
	if config.NodeUUID == "" {
		config.NodeUUID = generateUUID()
	}
	
	if config.Address == "" {
		config.Address = "tcp://*:0" // Auto-assign port
	}
	
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	
	if config.LingerMs <= 0 {
		config.LingerMs = 100
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	producer := &Producer{
		config:      config,
		uuid:        config.NodeUUID,
		topics:      make(map[string]*TopicState),
		records:     make(chan *ProduceRequest, 10000),
		batches:     make(chan *RecordBatch, 1000),
		commands:    make(chan producerCmd, 100),
		responses:   make(chan *ProduceResponse, 10000),
		acks:        make(chan *AckMessage, 10000),
		state:       NodeStateStopped,
		ctx:         ctx,
		cancel:      cancel,
		statistics:  &Statistics{},
	}
	
	return producer, nil
}

// Start begins producer operation
func (p *Producer) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	if p.state != NodeStateStopped {
		return fmt.Errorf("producer already started")
	}
	
	p.state = NodeStateStarting
	
	// Create and bind PUB socket for publishing records
	p.pubSocket = zmq4.NewPub(p.ctx)
	if err := p.pubSocket.Listen(p.config.Address); err != nil {
		return fmt.Errorf("failed to bind PUB socket: %w", err)
	}
	
	// Connect to tower if specified
	if p.config.TowerAddr != "" {
		p.towerSocket = zmq4.NewDealer(p.ctx)
		if err := p.towerSocket.Dial(p.config.TowerAddr); err != nil {
			return fmt.Errorf("failed to connect to tower: %w", err)
		}
	}
	
	// Start internal loops
	p.wg.Add(1)
	go p.commandLoop()
	
	p.wg.Add(1)
	go p.recordLoop()
	
	p.wg.Add(1)
	go p.batchLoop()
	
	p.wg.Add(1)
	go p.responseLoop()
	
	p.wg.Add(1)
	go p.ackLoop()
	
	p.wg.Add(1)
	go p.flushLoop()
	
	p.state = NodeStateRunning
	p.statistics.StartTime = time.Now()
	
	return nil
}

// Stop stops the producer
func (p *Producer) Stop() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	if p.state == NodeStateStopped {
		return
	}
	
	p.state = NodeStateStopping
	
	// Flush any pending batches
	p.flushAllBatches()
	
	// Cancel context and wait for goroutines
	p.cancel()
	p.wg.Wait()
	
	// Close sockets
	if p.pubSocket != nil {
		p.pubSocket.Close()
	}
	if p.towerSocket != nil {
		p.towerSocket.Close()
	}
	
	close(p.records)
	close(p.batches)
	close(p.commands)
	close(p.responses)
	close(p.acks)
	
	p.state = NodeStateStopped
}

// Produce publishes a record to a topic
func (p *Producer) Produce(topic string, key, value []byte, headers map[string]string) (*ProduceResponse, error) {
	response := make(chan *ProduceResponse, 1)
	
	request := &ProduceRequest{
		Topic:     topic,
		Key:       key,
		Value:     value,
		Headers:   headers,
		Timestamp: time.Now(),
		Response:  response,
	}
	
	select {
	case p.records <- request:
		// Request queued successfully
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	default:
		return nil, fmt.Errorf("producer queue full")
	}
	
	// Wait for response
	select {
	case resp := <-response:
		return resp, resp.Error
	case <-p.ctx.Done():
		return nil, p.ctx.Err()
	case <-time.After(DefaultStoreTimeout):
		return nil, fmt.Errorf("produce timeout")
	}
}

// ProduceAsync publishes a record asynchronously
func (p *Producer) ProduceAsync(topic string, key, value []byte, headers map[string]string, callback func(*ProduceResponse)) error {
	response := make(chan *ProduceResponse, 1)
	
	request := &ProduceRequest{
		Topic:     topic,
		Key:       key,
		Value:     value,
		Headers:   headers,
		Timestamp: time.Now(),
		Response:  response,
	}
	
	select {
	case p.records <- request:
		// Start goroutine to handle response
		go func() {
			resp := <-response
			if callback != nil {
				callback(resp)
			}
		}()
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	default:
		return fmt.Errorf("producer queue full")
	}
}

// Flush forces immediate publishing of all pending batches
func (p *Producer) Flush() error {
	reply := make(chan interface{}, 1)
	cmd := producerCmd{
		action: "flush",
		reply:  reply,
	}
	
	select {
	case p.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

// GetStatistics returns producer statistics
func (p *Producer) GetStatistics() *Statistics {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	
	stats := *p.statistics
	stats.LastUpdate = time.Now()
	return &stats
}

// commandLoop processes producer commands
func (p *Producer) commandLoop() {
	defer p.wg.Done()
	
	for {
		select {
		case cmd := <-p.commands:
			p.handleCommand(cmd)
		case <-p.ctx.Done():
			return
		}
	}
}

// recordLoop processes incoming record requests
func (p *Producer) recordLoop() {
	defer p.wg.Done()
	
	for {
		select {
		case request := <-p.records:
			p.processProduceRequest(request)
		case <-p.ctx.Done():
			return
		}
	}
}

// batchLoop processes batched records for publishing
func (p *Producer) batchLoop() {
	defer p.wg.Done()
	
	for {
		select {
		case batch := <-p.batches:
			p.publishBatch(batch)
		case <-p.ctx.Done():
			return
		}
	}
}

// responseLoop processes produce response notifications
func (p *Producer) responseLoop() {
	defer p.wg.Done()
	
	for {
		select {
		case response := <-p.responses:
			// Response handling is done in processProduceRequest
			_ = response
		case <-p.ctx.Done():
			return
		}
	}
}

// ackLoop processes acknowledgment messages
func (p *Producer) ackLoop() {
	defer p.wg.Done()
	
	for {
		select {
		case ack := <-p.acks:
			p.processAck(ack)
		case <-p.ctx.Done():
			return
		}
	}
}

// flushLoop periodically flushes pending batches
func (p *Producer) flushLoop() {
	defer p.wg.Done()
	
	ticker := time.NewTicker(time.Duration(p.config.LingerMs) * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			p.flushPendingBatches()
		case <-p.ctx.Done():
			return
		}
	}
}

// handleCommand processes a producer command
func (p *Producer) handleCommand(cmd producerCmd) {
	switch cmd.action {
	case "flush":
		p.flushAllBatches()
		cmd.reply <- nil
		
	case "get_topics":
		p.mutex.RLock()
		topics := make([]string, 0, len(p.topics))
		for topic := range p.topics {
			topics = append(topics, topic)
		}
		p.mutex.RUnlock()
		cmd.reply <- topics
	}
}

// processProduceRequest processes a produce request
func (p *Producer) processProduceRequest(request *ProduceRequest) {
	p.mutex.Lock()
	
	// Get or create topic state
	topicState, exists := p.topics[request.Topic]
	if !exists {
		topicState = &TopicState{
			Name:       request.Topic,
			NextOffset: 0,
			Headers:    make(map[string]string),
		}
		p.topics[request.Topic] = topicState
	}
	
	// Create record
	record := &Record{
		Topic:     request.Topic,
		Partition: p.uuid, // Producer UUID is partition identifier
		Offset:    topicState.NextOffset,
		Key:       request.Key,
		Value:     request.Value,
		Headers:   request.Headers,
		Timestamp: request.Timestamp,
	}
	
	// Increment offset
	topicState.NextOffset++
	
	// Add to batch
	if topicState.PendingBatch == nil {
		topicState.PendingBatch = &RecordBatch{
			Topic:      request.Topic,
			Partition:  p.uuid,
			Records:    make([]*Record, 0, p.config.BatchSize),
			BaseOffset: record.Offset,
			Timestamp:  time.Now(),
		}
	}
	
	topicState.PendingBatch.Records = append(topicState.PendingBatch.Records, record)
	topicState.PendingBatch.BatchSize++
	
	p.mutex.Unlock()
	
	// Send immediate response
	response := &ProduceResponse{
		Topic:     request.Topic,
		Partition: p.uuid,
		Offset:    record.Offset,
		Timestamp: time.Now(),
	}
	
	select {
	case request.Response <- response:
		// Response sent successfully
	case <-p.ctx.Done():
		return
	}
	
	// Check if batch should be flushed
	p.mutex.RLock()
	shouldFlush := topicState.PendingBatch.BatchSize >= p.config.BatchSize
	p.mutex.RUnlock()
	
	if shouldFlush {
		p.flushTopicBatch(request.Topic)
	}
	
	// Update statistics
	p.mutex.Lock()
	p.statistics.RecordsProduced++
	p.statistics.BytesProduced += uint64(len(record.Key) + len(record.Value))
	p.mutex.Unlock()
}

// publishBatch publishes a batch of records
func (p *Producer) publishBatch(batch *RecordBatch) {
	for _, record := range batch.Records {
		msg, err := CreateRecordMessage(record.Topic, record)
		if err != nil {
			p.statistics.ProduceErrors++
			continue
		}
		
		data, err := msg.Marshal()
		if err != nil {
			p.statistics.ProduceErrors++
			continue
		}
		
		zmqMsg := zmq4.NewMsgFromBytes(data)
		if err := p.pubSocket.SendMulti(zmqMsg); err != nil {
			p.statistics.ProduceErrors++
			continue
		}
		
		p.statistics.MessagesOut++
		p.statistics.BytesOut += uint64(len(data))
	}
}

// processAck processes an acknowledgment message
func (p *Producer) processAck(ack *AckMessage) {
	// Update statistics or handle delivery confirmation
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	// Implementation would track acknowledged offsets
	// For now, just update statistics
	_ = ack
}

// flushPendingBatches flushes batches that have been pending too long
func (p *Producer) flushPendingBatches() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	now := time.Now()
	lingerDuration := time.Duration(p.config.LingerMs) * time.Millisecond
	
	for topic, topicState := range p.topics {
		if topicState.PendingBatch != nil && 
		   now.Sub(topicState.PendingBatch.Timestamp) > lingerDuration {
			p.flushTopicBatchLocked(topic, topicState)
		}
	}
}

// flushTopicBatch flushes the pending batch for a specific topic
func (p *Producer) flushTopicBatch(topic string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	topicState, exists := p.topics[topic]
	if !exists || topicState.PendingBatch == nil {
		return
	}
	
	p.flushTopicBatchLocked(topic, topicState)
}

// flushTopicBatchLocked flushes a topic batch (must hold mutex)
func (p *Producer) flushTopicBatchLocked(topic string, topicState *TopicState) {
	if topicState.PendingBatch == nil || len(topicState.PendingBatch.Records) == 0 {
		return
	}
	
	// Send batch for publishing
	select {
	case p.batches <- topicState.PendingBatch:
		// Batch queued for publishing
		topicState.PendingBatch = nil
		topicState.LastFlush = time.Now()
	default:
		// Batch queue full - this is a problem
		p.statistics.ProduceErrors++
	}
}

// flushAllBatches flushes all pending batches
func (p *Producer) flushAllBatches() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	
	for topic, topicState := range p.topics {
		if topicState.PendingBatch != nil {
			p.flushTopicBatchLocked(topic, topicState)
		}
	}
}