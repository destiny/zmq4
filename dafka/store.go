// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dafka

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/destiny/zmq4/v25"
)

// Store represents a DAFKA store node with channel-managed persistence
type Store struct {
	// Configuration
	config    *StoreConfig     // Store configuration
	uuid      string           // Store UUID
	dataDir   string           // Data directory for persistence
	
	// Network components
	subSocket    zmq4.Socket    // SUB socket for receiving records
	dealerSocket zmq4.Socket    // DEALER socket for serving requests
	towerSocket  zmq4.Socket    // Socket for tower communication
	
	// Storage management
	partitions   map[string]*PartitionStore // Partition storage
	
	// Channel-based communication
	records      chan *StoreRecord      // Incoming records to store
	commands     chan storeCmd          // Store control commands
	fetchRequests chan *StoreFetchRequest // Fetch requests from consumers
	headRequests chan *HeadRequest      // HEAD requests
	syncRequests chan *SyncRequest      // Sync requests
	
	// State management
	state        int             // Store state
	ctx          context.Context // Context for cancellation
	cancel       context.CancelFunc // Cancel function
	wg           sync.WaitGroup  // Wait group for goroutines
	mutex        sync.RWMutex    // Protects store state
	statistics   *Statistics    // Store statistics
}

// PartitionStore manages storage for a single partition
type PartitionStore struct {
	Topic         string         // Topic name
	Partition     string         // Partition identifier
	HeadOffset    uint64         // Highest stored offset
	File          *os.File       // Log file for this partition
	Index         map[uint64]int64 // Offset -> file position index
	LastSync      time.Time      // Last sync to disk
	RecordCount   uint64         // Total records stored
	mutex         sync.RWMutex   // Protects partition state
}

// StoreRecord represents a record to be stored
type StoreRecord struct {
	Topic     string    // Topic name
	Partition string    // Partition identifier
	Record    *Record   // The record to store
	Response  chan error // Response channel
}

// StoreFetchRequest represents a fetch request to the store
type StoreFetchRequest struct {
	Topic     string    // Topic name
	Partition string    // Partition identifier
	Offset    uint64    // Starting offset
	Count     uint32    // Number of records to fetch
	Response  chan *StoreFetchResponse // Response channel
}

// StoreFetchResponse represents the response to a fetch request
type StoreFetchResponse struct {
	Records []*Record // Fetched records
	Error   error     // Error if fetch failed
}

// HeadRequest represents a request for HEAD information
type HeadRequest struct {
	Topic     string    // Topic name (empty for all topics)
	Partition string    // Partition identifier (empty for all partitions)
	Response  chan *HeadResponse // Response channel
}

// HeadResponse represents the response to a HEAD request
type HeadResponse struct {
	Heads []HeadInfo // HEAD information for partitions
	Error error      // Error if request failed
}

// HeadInfo contains HEAD information for a partition
type HeadInfo struct {
	Topic     string // Topic name
	Partition string // Partition identifier
	Offset    uint64 // Highest offset
}

// SyncRequest represents a request to sync data to disk
type SyncRequest struct {
	Topic     string    // Topic name (empty for all topics)
	Partition string    // Partition identifier (empty for all partitions)
	Response  chan error // Response channel
}

// storeCmd represents internal store commands
type storeCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// LogEntry represents a stored log entry
type LogEntry struct {
	Offset    uint64    // Record offset
	Timestamp int64     // Storage timestamp
	KeySize   uint32    // Key size
	ValueSize uint32    // Value size
	HeaderCount uint8   // Number of headers
	// Followed by key, value, and headers data
}

// NewStore creates a new DAFKA store
func NewStore(config *StoreConfig) (*Store, error) {
	if config == nil {
		return nil, fmt.Errorf("store config is required")
	}
	
	if config.NodeUUID == "" {
		config.NodeUUID = generateUUID()
	}
	
	if config.Address == "" {
		config.Address = "tcp://*:0" // Auto-assign port
	}
	
	if config.DataDir == "" {
		config.DataDir = "./dafka-store"
	}
	
	if config.SyncInterval == 0 {
		config.SyncInterval = 5 * time.Second
	}
	
	if config.RetentionMs == 0 {
		config.RetentionMs = 7 * 24 * 60 * 60 * 1000 // 7 days
	}
	
	// Create data directory
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	store := &Store{
		config:        config,
		uuid:          config.NodeUUID,
		dataDir:       config.DataDir,
		partitions:    make(map[string]*PartitionStore),
		records:       make(chan *StoreRecord, 10000),
		commands:      make(chan storeCmd, 100),
		fetchRequests: make(chan *StoreFetchRequest, 1000),
		headRequests:  make(chan *HeadRequest, 1000),
		syncRequests:  make(chan *SyncRequest, 100),
		state:         NodeStateStopped,
		ctx:           ctx,
		cancel:        cancel,
		statistics:    &Statistics{},
	}
	
	return store, nil
}

// Start begins store operation
func (s *Store) Start() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if s.state != NodeStateStopped {
		return fmt.Errorf("store already started")
	}
	
	s.state = NodeStateStarting
	
	// Load existing partitions
	if err := s.loadPartitions(); err != nil {
		return fmt.Errorf("failed to load partitions: %w", err)
	}
	
	// Create and bind SUB socket for receiving records
	s.subSocket = zmq4.NewSub(s.ctx)
	s.subSocket.SetOption(zmq4.OptionSubscribe, "") // Subscribe to all messages
	
	// Create and bind DEALER socket for serving requests
	s.dealerSocket = zmq4.NewDealer(s.ctx)
	if err := s.dealerSocket.Listen(s.config.Address); err != nil {
		return fmt.Errorf("failed to bind DEALER socket: %w", err)
	}
	
	// Connect to tower if specified
	if s.config.TowerAddr != "" {
		s.towerSocket = zmq4.NewDealer(s.ctx)
		if err := s.towerSocket.Dial(s.config.TowerAddr); err != nil {
			return fmt.Errorf("failed to connect to tower: %w", err)
		}
	}
	
	// Start internal loops
	s.wg.Add(1)
	go s.commandLoop()
	
	s.wg.Add(1)
	go s.subscriptionLoop()
	
	s.wg.Add(1)
	go s.recordLoop()
	
	s.wg.Add(1)
	go s.fetchLoop()
	
	s.wg.Add(1)
	go s.headLoop()
	
	s.wg.Add(1)
	go s.syncLoop()
	
	s.wg.Add(1)
	go s.periodicSyncLoop()
	
	s.state = NodeStateRunning
	s.statistics.StartTime = time.Now()
	
	return nil
}

// Stop stops the store
func (s *Store) Stop() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	
	if s.state == NodeStateStopped {
		return
	}
	
	s.state = NodeStateStopping
	
	// Sync all partitions
	s.syncAllPartitions()
	
	// Cancel context and wait for goroutines
	s.cancel()
	s.wg.Wait()
	
	// Close partition files
	for _, partition := range s.partitions {
		if partition.File != nil {
			partition.File.Close()
		}
	}
	
	// Close sockets
	if s.subSocket != nil {
		s.subSocket.Close()
	}
	if s.dealerSocket != nil {
		s.dealerSocket.Close()
	}
	if s.towerSocket != nil {
		s.towerSocket.Close()
	}
	
	close(s.records)
	close(s.commands)
	close(s.fetchRequests)
	close(s.headRequests)
	close(s.syncRequests)
	
	s.state = NodeStateStopped
}

// Store stores a record
func (s *Store) Store(record *Record) error {
	response := make(chan error, 1)
	
	storeRecord := &StoreRecord{
		Topic:     record.Topic,
		Partition: record.Partition,
		Record:    record,
		Response:  response,
	}
	
	select {
	case s.records <- storeRecord:
		// Request queued successfully
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
		return fmt.Errorf("store queue full")
	}
	
	// Wait for response
	select {
	case err := <-response:
		return err
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-time.After(DefaultStoreTimeout):
		return fmt.Errorf("store timeout")
	}
}

// Fetch fetches records from a partition
func (s *Store) Fetch(topic, partition string, offset uint64, count uint32) ([]*Record, error) {
	response := make(chan *StoreFetchResponse, 1)
	
	request := &StoreFetchRequest{
		Topic:     topic,
		Partition: partition,
		Offset:    offset,
		Count:     count,
		Response:  response,
	}
	
	select {
	case s.fetchRequests <- request:
		// Request queued successfully
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	default:
		return nil, fmt.Errorf("fetch queue full")
	}
	
	// Wait for response
	select {
	case resp := <-response:
		return resp.Records, resp.Error
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case <-time.After(DefaultFetchTimeout):
		return nil, fmt.Errorf("fetch timeout")
	}
}

// GetHeads returns HEAD information for partitions
func (s *Store) GetHeads(topic, partition string) ([]HeadInfo, error) {
	response := make(chan *HeadResponse, 1)
	
	request := &HeadRequest{
		Topic:     topic,
		Partition: partition,
		Response:  response,
	}
	
	select {
	case s.headRequests <- request:
		// Request queued successfully
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	default:
		return nil, fmt.Errorf("head queue full")
	}
	
	// Wait for response
	select {
	case resp := <-response:
		return resp.Heads, resp.Error
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case <-time.After(DefaultFetchTimeout):
		return nil, fmt.Errorf("head timeout")
	}
}

// Sync syncs data to disk
func (s *Store) Sync() error {
	response := make(chan error, 1)
	
	request := &SyncRequest{
		Response: response,
	}
	
	select {
	case s.syncRequests <- request:
		// Request queued successfully
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
		return fmt.Errorf("sync queue full")
	}
	
	// Wait for response
	select {
	case err := <-response:
		return err
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-time.After(DefaultStoreTimeout):
		return fmt.Errorf("sync timeout")
	}
}

// GetStatistics returns store statistics
func (s *Store) GetStatistics() *Statistics {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	stats := *s.statistics
	stats.LastUpdate = time.Now()
	return &stats
}

// commandLoop processes store commands
func (s *Store) commandLoop() {
	defer s.wg.Done()
	
	for {
		select {
		case cmd := <-s.commands:
			s.handleCommand(cmd)
		case <-s.ctx.Done():
			return
		}
	}
}

// subscriptionLoop processes incoming subscription messages
func (s *Store) subscriptionLoop() {
	defer s.wg.Done()
	
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// Receive messages from SUB socket
			msg, err := s.subSocket.Recv()
			if err != nil {
				continue
			}
			
			s.processSubscriptionMessage(msg.Bytes())
		}
	}
}

// recordLoop processes records to be stored
func (s *Store) recordLoop() {
	defer s.wg.Done()
	
	for {
		select {
		case storeRecord := <-s.records:
			err := s.storeRecord(storeRecord.Record)
			storeRecord.Response <- err
		case <-s.ctx.Done():
			return
		}
	}
}

// fetchLoop processes fetch requests
func (s *Store) fetchLoop() {
	defer s.wg.Done()
	
	for {
		select {
		case request := <-s.fetchRequests:
			s.processFetchRequest(request)
		case <-s.ctx.Done():
			return
		}
	}
}

// headLoop processes HEAD requests
func (s *Store) headLoop() {
	defer s.wg.Done()
	
	for {
		select {
		case request := <-s.headRequests:
			s.processHeadRequest(request)
		case <-s.ctx.Done():
			return
		}
	}
}

// syncLoop processes sync requests
func (s *Store) syncLoop() {
	defer s.wg.Done()
	
	for {
		select {
		case request := <-s.syncRequests:
			err := s.syncPartitions(request.Topic, request.Partition)
			request.Response <- err
		case <-s.ctx.Done():
			return
		}
	}
}

// periodicSyncLoop periodically syncs data to disk
func (s *Store) periodicSyncLoop() {
	defer s.wg.Done()
	
	ticker := time.NewTicker(s.config.SyncInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			s.syncAllPartitions()
		case <-s.ctx.Done():
			return
		}
	}
}

// handleCommand processes a store command
func (s *Store) handleCommand(cmd storeCmd) {
	switch cmd.action {
	case "get_partitions":
		s.mutex.RLock()
		partitions := make([]string, 0, len(s.partitions))
		for key := range s.partitions {
			partitions = append(partitions, key)
		}
		s.mutex.RUnlock()
		cmd.reply <- partitions
	}
}

// processSubscriptionMessage processes an incoming subscription message
func (s *Store) processSubscriptionMessage(data []byte) {
	// Parse DAFKA message
	var msg Message
	if err := msg.Unmarshal(data); err != nil {
		s.statistics.StoreErrors++
		return
	}
	
	// Only process RECORD messages
	if msg.Type != MessageTypeRecord {
		return
	}
	
	var recordMsg RecordMessage
	if err := recordMsg.Unmarshal(msg.Body); err != nil {
		s.statistics.StoreErrors++
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
		Timestamp: time.Now(),
	}
	
	// Store record asynchronously
	storeRecord := &StoreRecord{
		Topic:     record.Topic,
		Partition: record.Partition,
		Record:    record,
		Response:  make(chan error, 1),
	}
	
	select {
	case s.records <- storeRecord:
		// Don't wait for response in subscription loop
		go func() { <-storeRecord.Response }()
	default:
		// Store queue full
		s.statistics.StoreErrors++
	}
	
	s.statistics.MessagesIn++
	s.statistics.BytesIn += uint64(len(data))
}

// storeRecord stores a single record
func (s *Store) storeRecord(record *Record) error {
	partitionKey := fmt.Sprintf("%s:%s", record.Topic, record.Partition)
	
	s.mutex.Lock()
	partition, exists := s.partitions[partitionKey]
	if !exists {
		var err error
		partition, err = s.createPartition(record.Topic, record.Partition)
		if err != nil {
			s.mutex.Unlock()
			return err
		}
		s.partitions[partitionKey] = partition
	}
	s.mutex.Unlock()
	
	// Store record in partition
	if err := s.writeRecord(partition, record); err != nil {
		s.statistics.StoreErrors++
		return err
	}
	
	// Update statistics
	s.statistics.RecordsStored++
	s.statistics.BytesStored += uint64(len(record.Key) + len(record.Value))
	
	return nil
}

// createPartition creates a new partition storage
func (s *Store) createPartition(topic, partition string) (*PartitionStore, error) {
	filename := fmt.Sprintf("%s-%s.log", topic, partition)
	filepath := filepath.Join(s.dataDir, filename)
	
	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create partition file: %w", err)
	}
	
	partitionStore := &PartitionStore{
		Topic:       topic,
		Partition:   partition,
		HeadOffset:  0,
		File:        file,
		Index:       make(map[uint64]int64),
		LastSync:    time.Now(),
		RecordCount: 0,
	}
	
	// Load existing records to build index
	if err := s.loadPartitionIndex(partitionStore); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to load partition index: %w", err)
	}
	
	return partitionStore, nil
}

// writeRecord writes a record to a partition
func (s *Store) writeRecord(partition *PartitionStore, record *Record) error {
	partition.mutex.Lock()
	defer partition.mutex.Unlock()
	
	// Get current file position
	pos, err := partition.File.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("failed to seek to end: %w", err)
	}
	
	// Create log entry
	entry := LogEntry{
		Offset:      record.Offset,
		Timestamp:   time.Now().UnixNano(),
		KeySize:     uint32(len(record.Key)),
		ValueSize:   uint32(len(record.Value)),
		HeaderCount: uint8(len(record.Headers)),
	}
	
	// Write log entry header
	if err := binary.Write(partition.File, binary.BigEndian, entry); err != nil {
		return fmt.Errorf("failed to write log entry: %w", err)
	}
	
	// Write key
	if len(record.Key) > 0 {
		if _, err := partition.File.Write(record.Key); err != nil {
			return fmt.Errorf("failed to write key: %w", err)
		}
	}
	
	// Write value
	if len(record.Value) > 0 {
		if _, err := partition.File.Write(record.Value); err != nil {
			return fmt.Errorf("failed to write value: %w", err)
		}
	}
	
	// Write headers
	for key, value := range record.Headers {
		keyBytes := []byte(key)
		valueBytes := []byte(value)
		
		// Write header key length and key
		if err := binary.Write(partition.File, binary.BigEndian, uint16(len(keyBytes))); err != nil {
			return fmt.Errorf("failed to write header key length: %w", err)
		}
		if _, err := partition.File.Write(keyBytes); err != nil {
			return fmt.Errorf("failed to write header key: %w", err)
		}
		
		// Write header value length and value
		if err := binary.Write(partition.File, binary.BigEndian, uint16(len(valueBytes))); err != nil {
			return fmt.Errorf("failed to write header value length: %w", err)
		}
		if _, err := partition.File.Write(valueBytes); err != nil {
			return fmt.Errorf("failed to write header value: %w", err)
		}
	}
	
	// Update index
	partition.Index[record.Offset] = pos
	
	// Update partition state
	if record.Offset > partition.HeadOffset {
		partition.HeadOffset = record.Offset
	}
	partition.RecordCount++
	
	return nil
}

// processFetchRequest processes a fetch request
func (s *Store) processFetchRequest(request *StoreFetchRequest) {
	partitionKey := fmt.Sprintf("%s:%s", request.Topic, request.Partition)
	
	s.mutex.RLock()
	partition, exists := s.partitions[partitionKey]
	s.mutex.RUnlock()
	
	if !exists {
		request.Response <- &StoreFetchResponse{
			Error: fmt.Errorf("partition not found: %s", partitionKey),
		}
		return
	}
	
	records, err := s.readRecords(partition, request.Offset, request.Count)
	request.Response <- &StoreFetchResponse{
		Records: records,
		Error:   err,
	}
}

// processHeadRequest processes a HEAD request
func (s *Store) processHeadRequest(request *HeadRequest) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	var heads []HeadInfo
	
	for partitionKey, partition := range s.partitions {
		// Filter by topic and partition if specified
		if request.Topic != "" && partition.Topic != request.Topic {
			continue
		}
		if request.Partition != "" && partition.Partition != request.Partition {
			continue
		}
		
		partition.mutex.RLock()
		head := HeadInfo{
			Topic:     partition.Topic,
			Partition: partition.Partition,
			Offset:    partition.HeadOffset,
		}
		partition.mutex.RUnlock()
		
		heads = append(heads, head)
	}
	
	request.Response <- &HeadResponse{
		Heads: heads,
	}
}

// readRecords reads records from a partition
func (s *Store) readRecords(partition *PartitionStore, startOffset uint64, count uint32) ([]*Record, error) {
	partition.mutex.RLock()
	defer partition.mutex.RUnlock()
	
	var records []*Record
	
	for i := uint32(0); i < count; i++ {
		offset := startOffset + uint64(i)
		
		// Check if offset exists in index
		pos, exists := partition.Index[offset]
		if !exists {
			break // No more records
		}
		
		// Seek to position
		if _, err := partition.File.Seek(pos, io.SeekStart); err != nil {
			return records, fmt.Errorf("failed to seek to position: %w", err)
		}
		
		// Read record
		record, err := s.readRecord(partition.File, partition.Topic, partition.Partition)
		if err != nil {
			return records, fmt.Errorf("failed to read record: %w", err)
		}
		
		records = append(records, record)
	}
	
	return records, nil
}

// readRecord reads a single record from file
func (s *Store) readRecord(file *os.File, topic, partition string) (*Record, error) {
	// Read log entry header
	var entry LogEntry
	if err := binary.Read(file, binary.BigEndian, &entry); err != nil {
		return nil, fmt.Errorf("failed to read log entry: %w", err)
	}
	
	record := &Record{
		Topic:     topic,
		Partition: partition,
		Offset:    entry.Offset,
		Timestamp: time.Unix(0, entry.Timestamp),
		Headers:   make(map[string]string),
	}
	
	// Read key
	if entry.KeySize > 0 {
		record.Key = make([]byte, entry.KeySize)
		if _, err := file.Read(record.Key); err != nil {
			return nil, fmt.Errorf("failed to read key: %w", err)
		}
	}
	
	// Read value
	if entry.ValueSize > 0 {
		record.Value = make([]byte, entry.ValueSize)
		if _, err := file.Read(record.Value); err != nil {
			return nil, fmt.Errorf("failed to read value: %w", err)
		}
	}
	
	// Read headers
	for i := 0; i < int(entry.HeaderCount); i++ {
		// Read header key
		var keyLen uint16
		if err := binary.Read(file, binary.BigEndian, &keyLen); err != nil {
			return nil, fmt.Errorf("failed to read header key length: %w", err)
		}
		keyBytes := make([]byte, keyLen)
		if _, err := file.Read(keyBytes); err != nil {
			return nil, fmt.Errorf("failed to read header key: %w", err)
		}
		
		// Read header value
		var valueLen uint16
		if err := binary.Read(file, binary.BigEndian, &valueLen); err != nil {
			return nil, fmt.Errorf("failed to read header value length: %w", err)
		}
		valueBytes := make([]byte, valueLen)
		if _, err := file.Read(valueBytes); err != nil {
			return nil, fmt.Errorf("failed to read header value: %w", err)
		}
		
		record.Headers[string(keyBytes)] = string(valueBytes)
	}
	
	return record, nil
}

// loadPartitions loads existing partitions from disk
func (s *Store) loadPartitions() error {
	files, err := os.ReadDir(s.dataDir)
	if err != nil {
		return fmt.Errorf("failed to read data directory: %w", err)
	}
	
	for _, file := range files {
		if file.IsDir() || !filepath.Ext(file.Name()) == ".log" {
			continue
		}
		
		// Parse filename to extract topic and partition
		name := file.Name()
		name = name[:len(name)-4] // Remove .log extension
		
		// Simple parsing - in production, more robust parsing would be used
		// Format: topic-partition.log
		parts := []string{name} // Placeholder
		if len(parts) >= 2 {
			topic := parts[0]
			partition := parts[1]
			
			partitionStore, err := s.createPartition(topic, partition)
			if err != nil {
				return fmt.Errorf("failed to load partition %s:%s: %w", topic, partition, err)
			}
			
			partitionKey := fmt.Sprintf("%s:%s", topic, partition)
			s.partitions[partitionKey] = partitionStore
		}
	}
	
	return nil
}

// loadPartitionIndex loads the index for a partition
func (s *Store) loadPartitionIndex(partition *PartitionStore) error {
	// Seek to beginning of file
	if _, err := partition.File.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to start: %w", err)
	}
	
	for {
		// Get current position
		pos, err := partition.File.Seek(0, io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("failed to get position: %w", err)
		}
		
		// Try to read log entry header
		var entry LogEntry
		if err := binary.Read(partition.File, binary.BigEndian, &entry); err != nil {
			if err == io.EOF {
				break // End of file
			}
			return fmt.Errorf("failed to read log entry: %w", err)
		}
		
		// Update index
		partition.Index[entry.Offset] = pos
		
		// Update partition state
		if entry.Offset > partition.HeadOffset {
			partition.HeadOffset = entry.Offset
		}
		partition.RecordCount++
		
		// Skip record data
		skipSize := int64(entry.KeySize + entry.ValueSize)
		
		// Skip headers
		for i := 0; i < int(entry.HeaderCount); i++ {
			var keyLen, valueLen uint16
			if err := binary.Read(partition.File, binary.BigEndian, &keyLen); err != nil {
				return fmt.Errorf("failed to read header key length: %w", err)
			}
			if err := binary.Read(partition.File, binary.BigEndian, &valueLen); err != nil {
				return fmt.Errorf("failed to read header value length: %w", err)
			}
			skipSize += int64(keyLen + valueLen + 4) // +4 for length fields
		}
		
		if _, err := partition.File.Seek(skipSize, io.SeekCurrent); err != nil {
			return fmt.Errorf("failed to skip record data: %w", err)
		}
	}
	
	return nil
}

// syncPartitions syncs partitions to disk
func (s *Store) syncPartitions(topic, partition string) error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	
	for partitionKey, partitionStore := range s.partitions {
		// Filter by topic and partition if specified
		if topic != "" && partitionStore.Topic != topic {
			continue
		}
		if partition != "" && partitionStore.Partition != partition {
			continue
		}
		
		partitionStore.mutex.Lock()
		if partitionStore.File != nil {
			partitionStore.File.Sync()
			partitionStore.LastSync = time.Now()
		}
		partitionStore.mutex.Unlock()
	}
	
	return nil
}

// syncAllPartitions syncs all partitions to disk
func (s *Store) syncAllPartitions() {
	s.syncPartitions("", "")
}