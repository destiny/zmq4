// Copyright 2025 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dafka

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/destiny/zmq4/v25"
)

// Tower provides discovery and routing services for DAFKA mesh network
type Tower struct {
	// Configuration
	config    *TowerConfig    // Tower configuration
	uuid      string          // Tower UUID
	address   string          // Tower address
	
	// Network components
	pubSocket zmq4.Socket     // PUB socket for broadcasting
	xpubSocket zmq4.Socket    // XPUB socket for subscriptions
	beaconConn *net.UDPConn   // UDP beacon connection
	
	// Node registry
	nodes     map[string]*NodeInfo // Registered nodes
	topics    map[string]*TopicInfo // Topic information
	
	// Channel-based communication
	commands     chan towerCmd       // Tower control commands
	nodeUpdates  chan *NodeInfo      // Node registration updates
	topicUpdates chan *TopicInfo     // Topic information updates
	beacons      chan *TowerBeacon   // Beacon messages
	
	// State management
	state     int             // Tower state
	ctx       context.Context // Context for cancellation
	cancel    context.CancelFunc // Cancel function
	wg        sync.WaitGroup  // Wait group for goroutines
	mutex     sync.RWMutex    // Protects tower state
	statistics *Statistics    // Tower statistics
}

// towerCmd represents internal tower commands
type towerCmd struct {
	action string      // Command action
	data   interface{} // Command data
	reply  chan interface{} // Reply channel
}

// TowerBeacon represents a UDP beacon message
type TowerBeacon struct {
	UUID      string    // Tower UUID
	Address   string    // Tower address
	Topics    []string  // Available topics
	Timestamp time.Time // Beacon timestamp
}

// NewTower creates a new tower instance
func NewTower(config *TowerConfig) (*Tower, error) {
	if config == nil {
		return nil, fmt.Errorf("tower config is required")
	}
	
	if config.Address == "" {
		config.Address = "tcp://*:5670"
	}
	
	if config.BeaconPort == 0 {
		config.BeaconPort = DefaultTowerPort
	}
	
	if config.DiscoveryInterval == 0 {
		config.DiscoveryInterval = DefaultBeaconInterval
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	tower := &Tower{
		config:       config,
		uuid:         generateUUID(),
		address:      config.Address,
		nodes:        make(map[string]*NodeInfo),
		topics:       make(map[string]*TopicInfo),
		commands:     make(chan towerCmd, 100),
		nodeUpdates:  make(chan *NodeInfo, 1000),
		topicUpdates: make(chan *TopicInfo, 1000),
		beacons:      make(chan *TowerBeacon, 100),
		state:        NodeStateStopped,
		ctx:          ctx,
		cancel:       cancel,
		statistics:   &Statistics{},
	}
	
	return tower, nil
}

// Start begins tower operation
func (t *Tower) Start() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	
	if t.state != NodeStateStopped {
		return fmt.Errorf("tower already started")
	}
	
	t.state = NodeStateStarting
	
	// Create and bind PUB socket for broadcasting
	t.pubSocket = zmq4.NewPub(t.ctx)
	if err := t.pubSocket.Listen(t.address); err != nil {
		return fmt.Errorf("failed to bind PUB socket: %w", err)
	}
	
	// Create and bind XPUB socket for subscriptions
	xpubAddr := incrementPort(t.address, 1)
	t.xpubSocket = zmq4.NewXPub(t.ctx)
	if err := t.xpubSocket.Listen(xpubAddr); err != nil {
		return fmt.Errorf("failed to bind XPUB socket: %w", err)
	}
	
	// Set up UDP beacon
	if err := t.setupBeacon(); err != nil {
		return fmt.Errorf("failed to setup beacon: %w", err)
	}
	
	// Start internal loops
	t.wg.Add(1)
	go t.commandLoop()
	
	t.wg.Add(1)
	go t.nodeUpdateLoop()
	
	t.wg.Add(1)
	go t.topicUpdateLoop()
	
	t.wg.Add(1)
	go t.beaconLoop()
	
	t.wg.Add(1)
	go t.subscriptionLoop()
	
	t.state = NodeStateRunning
	t.statistics.StartTime = time.Now()
	
	return nil
}

// Stop stops the tower
func (t *Tower) Stop() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	
	if t.state == NodeStateStopped {
		return
	}
	
	t.state = NodeStateStopping
	
	// Cancel context and wait for goroutines
	t.cancel()
	t.wg.Wait()
	
	// Close sockets
	if t.pubSocket != nil {
		t.pubSocket.Close()
	}
	if t.xpubSocket != nil {
		t.xpubSocket.Close()
	}
	
	// Close UDP connection
	if t.beaconConn != nil {
		t.beaconConn.Close()
	}
	
	close(t.commands)
	close(t.nodeUpdates)
	close(t.topicUpdates)
	close(t.beacons)
	
	t.state = NodeStateStopped
}

// RegisterNode registers a node with the tower
func (t *Tower) RegisterNode(node *NodeInfo) error {
	reply := make(chan interface{}, 1)
	cmd := towerCmd{
		action: "register_node",
		data:   node,
		reply:  reply,
	}
	
	select {
	case t.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-t.ctx.Done():
		return t.ctx.Err()
	}
}

// UnregisterNode unregisters a node from the tower
func (t *Tower) UnregisterNode(nodeUUID string) error {
	reply := make(chan interface{}, 1)
	cmd := towerCmd{
		action: "unregister_node",
		data:   nodeUUID,
		reply:  reply,
	}
	
	select {
	case t.commands <- cmd:
		result := <-reply
		if err, ok := result.(error); ok {
			return err
		}
		return nil
	case <-t.ctx.Done():
		return t.ctx.Err()
	}
}

// GetNodes returns information about all registered nodes
func (t *Tower) GetNodes() map[string]*NodeInfo {
	reply := make(chan interface{}, 1)
	cmd := towerCmd{
		action: "get_nodes",
		reply:  reply,
	}
	
	select {
	case t.commands <- cmd:
		result := <-reply
		return result.(map[string]*NodeInfo)
	case <-t.ctx.Done():
		return make(map[string]*NodeInfo)
	}
}

// GetTopics returns information about all topics
func (t *Tower) GetTopics() map[string]*TopicInfo {
	reply := make(chan interface{}, 1)
	cmd := towerCmd{
		action: "get_topics",
		reply:  reply,
	}
	
	select {
	case t.commands <- cmd:
		result := <-reply
		return result.(map[string]*TopicInfo)
	case <-t.ctx.Done():
		return make(map[string]*TopicInfo)
	}
}

// GetStatistics returns tower statistics
func (t *Tower) GetStatistics() *Statistics {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	
	stats := *t.statistics
	stats.LastUpdate = time.Now()
	return &stats
}

// commandLoop processes tower commands
func (t *Tower) commandLoop() {
	defer t.wg.Done()
	
	for {
		select {
		case cmd := <-t.commands:
			t.handleCommand(cmd)
		case <-t.ctx.Done():
			return
		}
	}
}

// nodeUpdateLoop processes node registration updates
func (t *Tower) nodeUpdateLoop() {
	defer t.wg.Done()
	
	for {
		select {
		case nodeInfo := <-t.nodeUpdates:
			t.processNodeUpdate(nodeInfo)
		case <-t.ctx.Done():
			return
		}
	}
}

// topicUpdateLoop processes topic information updates
func (t *Tower) topicUpdateLoop() {
	defer t.wg.Done()
	
	for {
		select {
		case topicInfo := <-t.topicUpdates:
			t.processTopicUpdate(topicInfo)
		case <-t.ctx.Done():
			return
		}
	}
}

// beaconLoop handles beacon broadcasting and listening
func (t *Tower) beaconLoop() {
	defer t.wg.Done()
	
	ticker := time.NewTicker(t.config.DiscoveryInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			t.sendBeacon()
		case beacon := <-t.beacons:
			t.processBeacon(beacon)
		case <-t.ctx.Done():
			return
		}
	}
}

// subscriptionLoop handles XPUB subscriptions
func (t *Tower) subscriptionLoop() {
	defer t.wg.Done()
	
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			// Receive subscription messages
			msg, err := t.xpubSocket.Recv()
			if err != nil {
				continue
			}
			
			t.processSubscription(msg.Bytes())
		}
	}
}

// handleCommand processes a tower command
func (t *Tower) handleCommand(cmd towerCmd) {
	switch cmd.action {
	case "register_node":
		node := cmd.data.(*NodeInfo)
		t.mutex.Lock()
		t.nodes[node.UUID] = node
		t.mutex.Unlock()
		
		// Broadcast node registration
		t.broadcastNodeRegistration(node)
		cmd.reply <- nil
		
	case "unregister_node":
		nodeUUID := cmd.data.(string)
		t.mutex.Lock()
		delete(t.nodes, nodeUUID)
		t.mutex.Unlock()
		
		// Broadcast node unregistration
		t.broadcastNodeUnregistration(nodeUUID)
		cmd.reply <- nil
		
	case "get_nodes":
		t.mutex.RLock()
		nodes := make(map[string]*NodeInfo)
		for uuid, node := range t.nodes {
			nodeCopy := *node
			nodes[uuid] = &nodeCopy
		}
		t.mutex.RUnlock()
		cmd.reply <- nodes
		
	case "get_topics":
		t.mutex.RLock()
		topics := make(map[string]*TopicInfo)
		for name, topic := range t.topics {
			topicCopy := *topic
			topics[name] = &topicCopy
		}
		t.mutex.RUnlock()
		cmd.reply <- topics
	}
}

// processNodeUpdate processes a node registration update
func (t *Tower) processNodeUpdate(nodeInfo *NodeInfo) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	
	existingNode, exists := t.nodes[nodeInfo.UUID]
	t.nodes[nodeInfo.UUID] = nodeInfo
	
	if !exists {
		// New node
		t.statistics.Connections++
	} else {
		// Update existing node
		if !existingNode.Connected && nodeInfo.Connected {
			t.statistics.Reconnects++
		}
	}
	
	// Update topics this node is involved with
	for _, topicName := range nodeInfo.Topics {
		topic, exists := t.topics[topicName]
		if !exists {
			topic = &TopicInfo{
				Name:       topicName,
				Partitions: make(map[string]*PartitionInfo),
				CreatedAt:  time.Now(),
			}
			t.topics[topicName] = topic
		}
		topic.UpdatedAt = time.Now()
	}
}

// processTopicUpdate processes a topic information update
func (t *Tower) processTopicUpdate(topicInfo *TopicInfo) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	
	t.topics[topicInfo.Name] = topicInfo
}

// processBeacon processes a received beacon
func (t *Tower) processBeacon(beacon *TowerBeacon) {
	// Update tower registry or connect to remote tower
	// This would be used for tower federation
}

// processSubscription processes an XPUB subscription message
func (t *Tower) processSubscription(data []byte) {
	if len(data) == 0 {
		return
	}
	
	subscribe := data[0] == 1
	topic := string(data[1:])
	
	if subscribe {
		// Handle subscription
		t.statistics.MessagesIn++
	} else {
		// Handle unsubscription
	}
}

// broadcastNodeRegistration broadcasts a node registration
func (t *Tower) broadcastNodeRegistration(node *NodeInfo) {
	// Create and send registration message
	msg := zmq4.NewMsgFromBytes([]byte(fmt.Sprintf("NODE_REGISTER:%s:%s", node.UUID, node.Address)))
	t.pubSocket.SendMulti(msg)
	t.statistics.MessagesOut++
}

// broadcastNodeUnregistration broadcasts a node unregistration
func (t *Tower) broadcastNodeUnregistration(nodeUUID string) {
	// Create and send unregistration message
	msg := zmq4.NewMsgFromBytes([]byte(fmt.Sprintf("NODE_UNREGISTER:%s", nodeUUID)))
	t.pubSocket.SendMulti(msg)
	t.statistics.MessagesOut++
}

// setupBeacon sets up UDP beacon broadcasting
func (t *Tower) setupBeacon() error {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", t.config.BeaconPort))
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}
	
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to create UDP socket: %w", err)
	}
	
	t.beaconConn = conn
	
	// Start beacon listener
	t.wg.Add(1)
	go t.beaconListener()
	
	return nil
}

// beaconListener listens for incoming beacons
func (t *Tower) beaconListener() {
	defer t.wg.Done()
	
	buffer := make([]byte, 1024)
	
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
			t.beaconConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			
			n, addr, err := t.beaconConn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				continue
			}
			
			beacon := t.parseBeacon(buffer[:n], addr.IP)
			if beacon != nil {
				select {
				case t.beacons <- beacon:
				default:
					// Channel full, drop beacon
				}
			}
		}
	}
}

// sendBeacon sends a UDP beacon
func (t *Tower) sendBeacon() {
	beacon := t.createBeacon()
	data := t.marshalBeacon(beacon)
	
	broadcastAddr := &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: int(t.config.BeaconPort),
	}
	
	t.beaconConn.WriteToUDP(data, broadcastAddr)
}

// createBeacon creates a beacon message
func (t *Tower) createBeacon() *TowerBeacon {
	t.mutex.RLock()
	defer t.mutex.RUnlock()
	
	topics := make([]string, 0, len(t.topics))
	for topicName := range t.topics {
		topics = append(topics, topicName)
	}
	
	return &TowerBeacon{
		UUID:      t.uuid,
		Address:   t.address,
		Topics:    topics,
		Timestamp: time.Now(),
	}
}

// marshalBeacon marshals a beacon to bytes
func (t *Tower) marshalBeacon(beacon *TowerBeacon) []byte {
	// Simple beacon format: signature + UUID + address length + address + topic count + topics
	buf := make([]byte, 0, 512)
	
	// Signature
	buf = append(buf, 'D', 'A', 'F', 'K', 'A')
	
	// UUID (first 16 bytes)
	uuid := []byte(beacon.UUID)
	if len(uuid) > 16 {
		uuid = uuid[:16]
	}
	for len(uuid) < 16 {
		uuid = append(uuid, 0)
	}
	buf = append(buf, uuid...)
	
	// Address
	addrBytes := []byte(beacon.Address)
	buf = append(buf, byte(len(addrBytes)))
	buf = append(buf, addrBytes...)
	
	// Topic count
	buf = append(buf, byte(len(beacon.Topics)))
	
	// Topics
	for _, topic := range beacon.Topics {
		topicBytes := []byte(topic)
		if len(topicBytes) > 255 {
			topicBytes = topicBytes[:255]
		}
		buf = append(buf, byte(len(topicBytes)))
		buf = append(buf, topicBytes...)
	}
	
	return buf
}

// parseBeacon parses a beacon from bytes
func (t *Tower) parseBeacon(data []byte, sourceIP net.IP) *TowerBeacon {
	if len(data) < 22 { // Minimum: 5 sig + 16 uuid + 1 addr_len
		return nil
	}
	
	// Check signature
	if string(data[:5]) != "DAFKA" {
		return nil
	}
	
	offset := 5
	
	// Parse UUID
	uuid := string(data[offset : offset+16])
	offset += 16
	
	// Don't process our own beacons
	if uuid == t.uuid {
		return nil
	}
	
	// Parse address
	if offset >= len(data) {
		return nil
	}
	addrLen := int(data[offset])
	offset++
	
	if offset+addrLen > len(data) {
		return nil
	}
	address := string(data[offset : offset+addrLen])
	offset += addrLen
	
	// Parse topics
	if offset >= len(data) {
		return nil
	}
	topicCount := int(data[offset])
	offset++
	
	topics := make([]string, 0, topicCount)
	for i := 0; i < topicCount && offset < len(data); i++ {
		topicLen := int(data[offset])
		offset++
		
		if offset+topicLen > len(data) {
			break
		}
		
		topic := string(data[offset : offset+topicLen])
		topics = append(topics, topic)
		offset += topicLen
	}
	
	return &TowerBeacon{
		UUID:      uuid,
		Address:   address,
		Topics:    topics,
		Timestamp: time.Now(),
	}
}

// Helper functions

// generateUUID generates a simple UUID for tower identification
func generateUUID() string {
	return fmt.Sprintf("tower-%d", time.Now().UnixNano())
}

// incrementPort increments the port number in an address string
func incrementPort(address string, increment int) string {
	// Simple implementation - in production, proper URL parsing would be used
	return address // Placeholder
}